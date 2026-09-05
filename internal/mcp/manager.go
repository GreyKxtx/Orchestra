package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
)

// Manager starts and manages multiple MCP server connections.
//
// M26 in audit ledger: each client is paired with its original config so
// Manager.Call can lazy-restart a server whose subprocess died mid-session
// (capped at maxMCPRestarts per server per Manager lifetime). The cached
// tools list is refreshed on restart; if the post-restart schema differs
// from the pre-restart cache, a warning is logged so the operator can
// notice a server that's silently rotating its surface.
type Manager struct {
	mu      sync.Mutex
	clients []ServerClient
	entries []*serverSlot
	// resCache holds each server's resource list, discovered once. A nil
	// entry that is present means "asked, and there are none".
	resCache map[string][]MCPResource
	// promptCache is the same, for prompts/list.
	promptCache map[string][]MCPPrompt
}

// ServerClient is the surface the Manager drives, satisfied by the stdio
// Client and by RemoteClient (Streamable HTTP). Keeping the seam here rather
// than inside Client means the remote transport did not have to touch the
// working stdio path at all.
type ServerClient interface {
	ServerName() string
	Tools() []MCPTool
	AllToolNames() []string
	SetAllowedTools(names []string)
	Call(ctx context.Context, toolName string, arguments json.RawMessage) (string, error)
	IsDead() bool
	StderrTail() string
	Close() error
}

var (
	_ ServerClient = (*Client)(nil)
	_ ServerClient = (*RemoteClient)(nil)
)

// startServer connects to one configured server over whichever transport it
// declares. config.validateMCP has already guaranteed exactly one of them.
func startServer(ctx context.Context, cfg config.MCPServerConfig, opts StartOptions) (ServerClient, error) {
	if url := strings.TrimSpace(cfg.URL); url != "" {
		return StartRemote(ctx, RemoteConfig{
			Name:           cfg.Name,
			URL:            url,
			BearerTokenEnv: cfg.BearerTokenEnv,
			Headers:        cfg.Headers,
		}, opts)
	}
	return Start(ctx, cfg.Name, cfg.Command, cfg.Env, opts)
}

type serverSlot struct {
	cfg          config.MCPServerConfig
	restartCount int
}

const maxMCPRestarts = 3

// mcpStartTimeout caps each server's initialize+listTools handshake so a
// stuck server cannot block Core / apply startup. C5 in audit ledger.
const mcpStartTimeout = 30 * time.Second

// NewManager starts all enabled MCP servers from the config in parallel.
// Each server gets its own 30s timeout — a single stuck server no longer
// blocks the rest (was: serial start, no timeout = N stuck servers = hang
// for N×infinity). Non-fatal errors (individual server startup failures)
// are returned for the caller to log; they don't abort Core construction.
func NewManager(ctx context.Context, cfg config.MCPConfig) (*Manager, []error) {
	m := &Manager{}
	type startRes struct {
		idx int
		c   ServerClient
		err error
	}
	var enabled []config.MCPServerConfig
	var errs []error
	for _, srv := range cfg.Servers {
		if srv.Disabled {
			continue
		}
		// A server declaring neither transport used to be dropped in silence,
		// so a typo'd key meant the server simply never appeared. Report it.
		if len(srv.Command) == 0 && strings.TrimSpace(srv.URL) == "" {
			errs = append(errs, fmt.Errorf("mcp server %q: no transport configured — set command for a local server or url for a remote one", srv.Name))
			continue
		}
		enabled = append(enabled, srv)
	}
	if len(enabled) == 0 {
		return m, errs
	}

	results := make([]startRes, len(enabled))
	var wg sync.WaitGroup
	for i, srv := range enabled {
		wg.Add(1)
		go func(idx int, srv config.MCPServerConfig) {
			defer wg.Done()
			startCtx, cancel := context.WithTimeout(ctx, mcpStartTimeout)
			defer cancel()
			var opts StartOptions
			if srv.CallTimeoutS > 0 {
				opts.CallTimeout = time.Duration(srv.CallTimeoutS) * time.Second
			}
			c, err := startServer(startCtx, srv, opts)
			if err == nil && c != nil && len(srv.AllowedTools) > 0 {
				c.SetAllowedTools(srv.AllowedTools)
			}
			results[idx] = startRes{idx: idx, c: c, err: err}
		}(i, srv)
	}
	wg.Wait()

	for i, r := range results {
		if r.err != nil {
			errs = append(errs, fmt.Errorf("mcp server %q: %w", enabled[i].Name, r.err))
			continue
		}
		m.clients = append(m.clients, r.c)
		m.entries = append(m.entries, &serverSlot{cfg: enabled[i]})
	}
	return m, errs
}

// IsEmpty reports whether there are no active MCP server connections.
func (m *Manager) IsEmpty() bool {
	return m == nil || len(m.clients) == 0
}

// Close stops all MCP server subprocesses, logging any errors per server.
// M33 in audit ledger: previously `_ = c.Close()` discarded the kill-
// after-timeout error and cmd.Wait() result, hiding zombie / failed-kill
// outcomes from the operator.
func (m *Manager) Close() {
	if m == nil {
		return
	}
	for _, c := range m.clients {
		if err := c.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "mcp: server %q close: %v\n", c.ServerName(), err)
		}
	}
}

// ListToolDefs returns OpenAI-compatible tool definitions for all MCP tools.
// Tool names are prefixed as "mcp:<server>:<tool>" to avoid collisions.
func (m *Manager) ListToolDefs() []llm.ToolDef {
	if m.IsEmpty() {
		return nil
	}
	var out []llm.ToolDef
	for _, c := range m.clients {
		out = append(out, m.resourceToolDefs(c)...)
		for _, t := range c.Tools() {
			prefixedName := "mcp:" + c.ServerName() + ":" + t.Name
			schema := t.InputSchema
			if len(schema) == 0 {
				schema = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			out = append(out, llm.ToolDef{
				Type: "function",
				Function: llm.ToolFunctionDef{
					Name:        prefixedName,
					Description: t.Description,
					Parameters:  schema,
				},
			})
		}
	}
	return out
}

// Call routes "mcp:<server>:<tool>" calls to the appropriate server.
// M26 in audit ledger: when the target client's subprocess has died, an
// auto-restart is attempted (capped) before failing the call. A successful
// restart re-runs the same call once; further failures bubble up.
func (m *Manager) Call(ctx context.Context, prefixedName string, input json.RawMessage) (json.RawMessage, error) {
	serverName, toolName, err := parseMCPToolName(prefixedName)
	if err != nil {
		return nil, err
	}
	c := m.findClient(serverName)
	if c == nil {
		return nil, fmt.Errorf("mcp server %q not found", serverName)
	}
	if c.IsDead() {
		newClient, restartErr := m.maybeRestart(ctx, serverName)
		if restartErr != nil {
			return nil, fmt.Errorf("mcp server %q died and restart failed: %w", serverName, restartErr)
		}
		c = newClient
	}
	if out, handled, err := m.callResourceTool(ctx, c, toolName, input); handled {
		return out, err
	}
	result, images, err := callServer(ctx, c, toolName, input)
	if err != nil {
		return nil, err
	}
	payload := mcpCallPayload{Result: result}
	for _, img := range images {
		payload.Images = append(payload.Images, mcpImagePayload{
			Data: base64.StdEncoding.EncodeToString(img.Data),
			MIME: img.MIME,
		})
	}
	out, _ := json.Marshal(payload)
	return out, nil
}

// mcpCallPayload is the tool result the agent receives. Images do not belong
// in the text the model reads, but they have to travel through this result to
// reach the conversation — the tool interface returns JSON and nothing else —
// so the agent lifts them out into a real image message.
type mcpCallPayload struct {
	Result string            `json:"result"`
	Images []mcpImagePayload `json:"images,omitempty"`
}

type mcpImagePayload struct {
	Data string `json:"data"` // base64
	MIME string `json:"mime"`
}

// richCaller is the optional half of ServerClient: a client that can also
// return images. Both shipped clients implement it; keeping it optional means
// something that only implements Call is still a valid ServerClient.
type richCaller interface {
	CallRich(ctx context.Context, toolName string, arguments json.RawMessage) (string, []MCPImage, error)
}

func callServer(ctx context.Context, c ServerClient, toolName string, input json.RawMessage) (string, []MCPImage, error) {
	if rc, ok := c.(richCaller); ok {
		return rc.CallRich(ctx, toolName, input)
	}
	text, err := c.Call(ctx, toolName, input)
	return text, nil, err
}

// maybeRestart re-spawns a dead MCP client using its original config.
// Returns the fresh client on success. Capped at maxMCPRestarts per
// server per Manager lifetime so a permanently-broken server doesn't
// loop forever. On restart, the new tool list is compared with the
// cached one and a warning is logged on schema drift.
func (m *Manager) maybeRestart(ctx context.Context, serverName string) (ServerClient, error) {
	m.mu.Lock()
	var slot *serverSlot
	var idx int
	for i, c := range m.clients {
		if c.ServerName() == serverName {
			slot = m.entries[i]
			idx = i
			break
		}
	}
	if slot == nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("no slot for server %q", serverName)
	}
	if slot.restartCount >= maxMCPRestarts {
		m.mu.Unlock()
		return nil, fmt.Errorf("server %q exceeded max restarts (%d)", serverName, maxMCPRestarts)
	}
	slot.restartCount++
	cfg := slot.cfg
	oldClient := m.clients[idx]
	m.mu.Unlock()

	// Spawn outside the lock so concurrent calls to other servers don't
	// block on a slow restart handshake.
	startCtx, cancel := context.WithTimeout(ctx, mcpStartTimeout)
	defer cancel()
	var opts StartOptions
	if cfg.CallTimeoutS > 0 {
		opts.CallTimeout = time.Duration(cfg.CallTimeoutS) * time.Second
	}
	fresh, err := startServer(startCtx, cfg, opts)
	if err != nil {
		return nil, err
	}
	if len(cfg.AllowedTools) > 0 {
		fresh.SetAllowedTools(cfg.AllowedTools)
	}

	// Schema drift warning: compare bare tool names of old vs new.
	if oldClient != nil && !sameToolNames(oldClient.AllToolNames(), fresh.AllToolNames()) {
		fmt.Fprintf(os.Stderr, "mcp: server %q restarted with different tool list; agents may receive stale cached schemas until next ListToolDefs\n", serverName)
	}

	m.mu.Lock()
	m.clients[idx] = fresh
	delete(m.resCache, serverName) // a restarted server may offer a different set
	delete(m.promptCache, serverName)
	m.mu.Unlock()
	return fresh, nil
}

func sameToolNames(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]bool, len(a))
	for _, name := range a {
		seen[name] = true
	}
	for _, name := range b {
		if !seen[name] {
			return false
		}
	}
	return true
}

// IsMCPTool reports whether a tool name is an MCP-prefixed tool.
func IsMCPTool(name string) bool {
	return strings.HasPrefix(name, "mcp:")
}

func (m *Manager) findClient(serverName string) ServerClient {
	for _, c := range m.clients {
		if c.ServerName() == serverName {
			return c
		}
	}
	return nil
}

// ServerStatus describes one connected MCP server's runtime view.
type ServerStatus struct {
	Name      string
	ToolCount int      // tools currently exposed (after allowlist)
	Tools     []string // all discovered tool names (before allowlist)
	Dead      bool
}

// RuntimeStatuses returns status for each live client connection.
func (m *Manager) RuntimeStatuses() []ServerStatus {
	if m == nil || len(m.clients) == 0 {
		return nil
	}
	out := make([]ServerStatus, 0, len(m.clients))
	for _, c := range m.clients {
		out = append(out, ServerStatus{
			Name:      c.ServerName(),
			ToolCount: len(c.Tools()),
			Tools:     c.AllToolNames(),
			Dead:      c.IsDead(),
		})
	}
	return out
}

// FindClient returns the client for serverName, or nil.
func (m *Manager) FindClient(serverName string) ServerClient {
	return m.findClient(serverName)
}

func parseMCPToolName(name string) (serverName, toolName string, err error) {
	// Format: "mcp:<server>:<tool>" — tool name may contain colons itself
	parts := strings.SplitN(name, ":", 3)
	if len(parts) != 3 || parts[0] != "mcp" {
		return "", "", fmt.Errorf("invalid mcp tool name %q (expected mcp:<server>:<tool>)", name)
	}
	return parts[1], parts[2], nil
}
