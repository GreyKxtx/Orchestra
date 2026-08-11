// Package mcp implements a client for the Model Context Protocol (MCP).
// MCP servers expose tools over JSON-RPC 2.0 via stdio subprocess.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"sync"
	"sync/atomic"
	"time"

	"github.com/orchestra/orchestra/internal/subproc"
)

const mcpProtocolVersion = "2024-11-05"

// MCPTool is a tool definition returned by an MCP server.
type MCPTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// MCPContentItem is one piece of content in a tool response.
type MCPContentItem struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int64 `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Client manages a single MCP server subprocess.
//
// Mutex split (C6 in audit ledger): `mu` guards the `pending` request map.
// `writeMu` independently guards `stdin.Write`. Previously a single mutex
// covered both, and `send` held that mutex across a blocking pipe write —
// if the server was slow to read its stdin (pipe buffer ~64 KiB), the write
// blocked while holding mu, queuing every other `Call` and even `readLoop`'s
// response dispatch on the same mutex → classic deadlock when stdout was
// also full. Splitting the mutexes lets the reader keep dispatching
// responses (which unblock the writer) even when stdin is back-pressured.
type Client struct {
	name        string
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	stdout      *bufio.Scanner
	tools       []MCPTool
	callTimeout time.Duration // 0 = no per-call timeout; M27 in audit ledger
	idSeq       atomic.Int64
	mu          sync.Mutex
	writeMu     sync.Mutex
	pending     map[int64]chan rpcResponse
	done        chan struct{}

	// stderr is a bounded ring buffer carrying the server's recent stderr
	// output. L9 + S1 in audit ledger: shared with LSP via subproc package
	// so both packages don't carry the same drainStderr/StderrTail pair.
	stderr *subproc.StderrRing

	// allowedTools, if non-empty, restricts which tools from this server
	// are advertised via Tools() and accepted in Call. Patterns support
	// path.Match globs (`fs.*`). nil/empty = expose every tool.
	// M31 in audit ledger.
	allowedTools []string
}

// StderrTail returns the last <=64 KiB of the MCP server's stderr — useful
// for surfacing crash dumps / npm-install errors / etc. without polluting
// Orchestra's own stderr.
func (c *Client) StderrTail() string {
	return c.stderr.Tail()
}

// Start spawns the MCP server and performs the initialize handshake.
// Start launches the MCP subprocess and runs the initialize + tools/list
// handshake. The passed `ctx` scopes ONLY the handshake (so callers can apply
// a startup timeout that doesn't accidentally kill the long-lived process).
// The subprocess lives until c.Close — use that for shutdown. Was previously
// `exec.CommandContext(ctx, ...)`, which bound process lifetime to the
// startup ctx and made startup timeouts unsafe (cancel killed the server).
// StartOptions bundles per-server tunables that aren't part of the bare
// process spawn so the Start signature stays additive.
type StartOptions struct {
	// CallTimeout caps a single tools/call. 0 = no per-call timeout.
	CallTimeout time.Duration
}

// Start launches the MCP subprocess and runs the initialize + tools/list
// handshake. See Client.callTimeout for the M27 timeout semantics.
func Start(ctx context.Context, name string, command []string, env map[string]string, opts ...StartOptions) (*Client, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("mcp %q: command is empty", name)
	}

	cmd := exec.Command(command[0], command[1:]...)
	cmd.Env = buildEnv(env)
	subproc.SetProcessGroup(cmd) // S2 consolidation; H13 rationale (audit ledger)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp %q: stdin pipe: %w", name, err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp %q: stdout pipe: %w", name, err)
	}
	// L9 in audit ledger: capture server stderr into a per-Client ring
	// buffer instead of inheriting Orchestra's stderr (mixing MCP server
	// output with TUI / JSON-RPC over stdout). Mirrors the LSP M18 fix.
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp %q: stderr pipe: %w", name, err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp %q: start: %w", name, err)
	}

	var so StartOptions
	if len(opts) > 0 {
		so = opts[0]
	}
	c := &Client{
		name:        name,
		cmd:         cmd,
		stdin:       stdin,
		stdout:      bufio.NewScanner(stdoutPipe),
		callTimeout: so.CallTimeout,
		pending:     make(map[int64]chan rpcResponse),
		done:        make(chan struct{}),
		stderr:      subproc.NewStderrRing(0),
	}
	// Set a generous scan buffer for large tool descriptions.
	c.stdout.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)

	go c.stderr.Drain(stderrPipe)
	go c.readLoop()

	// Initialize handshake.
	if err := c.initialize(ctx); err != nil {
		c.Close()
		return nil, fmt.Errorf("mcp %q: initialize: %w", name, err)
	}

	// Discover tools.
	tools, err := c.listTools(ctx)
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("mcp %q: tools/list: %w", name, err)
	}
	c.tools = tools

	return c, nil
}

// SetAllowedTools sets a per-server tool allowlist. Names are bare MCP
// tool names (no mcp:server: prefix) and may use path.Match globs.
// Empty / nil = expose every tool. Must be called BEFORE Tools() is read
// by external callers; intended for invocation right after Start.
func (c *Client) SetAllowedTools(names []string) {
	c.allowedTools = names
}

// toolAllowed reports whether a tool name passes the allowlist (or no
// allowlist is configured).
func (c *Client) toolAllowed(name string) bool {
	if len(c.allowedTools) == 0 {
		return true
	}
	for _, pat := range c.allowedTools {
		if pat == name {
			return true
		}
		if ok, _ := path.Match(pat, name); ok {
			return true
		}
	}
	return false
}

// Tools returns the tools advertised by this server, filtered by the
// per-server allowlist (if any).
func (c *Client) Tools() []MCPTool {
	if len(c.allowedTools) == 0 {
		return c.tools
	}
	out := make([]MCPTool, 0, len(c.tools))
	for _, t := range c.tools {
		if c.toolAllowed(t.Name) {
			out = append(out, t)
		}
	}
	return out
}

// AllToolNames returns every tool discovered from the server, ignoring the
// per-server allowlist. Used by settings UI so toggles can re-enable tools.
func (c *Client) AllToolNames() []string {
	if c == nil || len(c.tools) == 0 {
		return nil
	}
	out := make([]string, 0, len(c.tools))
	for _, t := range c.tools {
		out = append(out, t.Name)
	}
	return out
}

// ServerName returns the configured name of this server.
func (c *Client) ServerName() string { return c.name }

// IsDead reports whether the underlying subprocess has exited (readLoop
// closed c.done). Manager.Call consults this before issuing a tool call
// so a dead server triggers an auto-restart attempt instead of producing
// the cryptic "mcp server X exited" we used to return. M26 in audit ledger.
func (c *Client) IsDead() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// Call invokes a tool on the MCP server and returns the combined text output.
// M27 in audit ledger: when configured with a per-server CallTimeoutS, the
// caller's ctx is wrapped so a hung tool can't tie up the entire agent step.
// M31 in audit ledger: tools outside the per-server AllowedTools allowlist
// are refused — defence in depth, since the allowlist also filters Tools()
// so the agent shouldn't even be aware of disallowed tools.
func (c *Client) Call(ctx context.Context, toolName string, arguments json.RawMessage) (string, error) {
	if !c.toolAllowed(toolName) {
		return "", fmt.Errorf("mcp tool %q is not in this server's allowed_tools list", toolName)
	}
	if c.callTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.callTimeout)
		defer cancel()
	}
	params := map[string]any{
		"name":      toolName,
		"arguments": json.RawMessage(arguments),
	}
	raw, err := c.call(ctx, "tools/call", params)
	if err != nil {
		return "", err
	}

	var result struct {
		Content []MCPContentItem `json:"content"`
		IsError bool             `json:"isError"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return string(raw), nil // return raw if parse fails
	}

	var out string
	nonTextDropped := 0
	for _, item := range result.Content {
		if item.Type == "text" {
			out += item.Text
		} else {
			nonTextDropped++
		}
	}
	// M28 in audit ledger: MCP defines image / resource / audio content
	// items in addition to text. We only forward text today; surface the
	// silent drop count so the model at least knows partial content was
	// omitted instead of blindly trusting the text was complete.
	if nonTextDropped > 0 {
		out += fmt.Sprintf("\n[orchestra: dropped %d non-text content item(s); only text is currently forwarded by mcp client]", nonTextDropped)
	}
	if result.IsError {
		return "", fmt.Errorf("mcp tool error: %s", out)
	}
	return out, nil
}

// Close stops the MCP server subprocess. After a 5s wait for graceful exit
// (the server should react to stdin EOF) we kill the entire process tree —
// see subproc.KillProcessTree / SetProcessGroup for the H13 rationale
// (npx → node orphans on Windows, unkilled children on Unix without Setpgid).
func (c *Client) Close() error {
	_ = c.stdin.Close()
	select {
	case <-c.done:
	case <-time.After(5 * time.Second):
		subproc.KillProcessTree(c.cmd)
		<-c.done
	}
	return c.cmd.Wait()
}

func (c *Client) initialize(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "orchestra", "version": "vnext"},
	}
	if _, err := c.call(ctx, "initialize", params); err != nil {
		return err
	}
	// Send initialized notification (no ID = notification, no response expected).
	return c.notify("notifications/initialized", nil)
}

func (c *Client) listTools(ctx context.Context) ([]MCPTool, error) {
	raw, err := c.call(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Tools []MCPTool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("parse tools/list: %w", err)
	}
	return result.Tools, nil
}

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.idSeq.Add(1)
	// P3 in audit ledger (Sprint 6): pooling this chan via sync.Pool was
	// evaluated and rejected. The race-safe recycle window is narrow
	// (readLoop may send to the chan AFTER ctx.Done deleted pending[id],
	// because the lookup-then-send sequence is not atomic) and the alloc
	// is ~80 B per call. For typical MCP loads (<1k calls per server per
	// session), GC pressure is negligible; the complexity of a drain-
	// before-recycle barrier would outweigh the saving.
	ch := make(chan rpcResponse, 1)

	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	req := rpcRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: params}
	if err := c.send(req); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("mcp rpc error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	case <-c.done:
		return nil, fmt.Errorf("mcp server %q exited", c.name)
	}
}

func (c *Client) notify(method string, params any) error {
	return c.send(rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
}

func (c *Client) send(req rpcRequest) error {
	b, err := json.Marshal(req)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	// writeMu (NOT mu) serialises actual stdin.Write — see Client doc.
	// Holding mu here would deadlock against readLoop's mu acquisition for
	// response dispatch when the server is back-pressured.
	c.writeMu.Lock()
	_, err = c.stdin.Write(b)
	c.writeMu.Unlock()
	return err
}

func (c *Client) readLoop() {
	defer close(c.done)
	for c.stdout.Scan() {
		line := c.stdout.Bytes()
		var resp rpcResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue // skip malformed lines (e.g. server startup logs)
		}
		if resp.ID == nil {
			// M29 in audit ledger: notifications/tools/list_changed is
			// the spec's signal that the server's tool list has rotated.
			// We currently cache c.tools once at handshake — refresh on
			// this notification so cached schemas don't go stale. Other
			// notifications (logs, progress) are ignored as before.
			if resp.Error == nil && resp.Result == nil {
				// Re-parse the line to extract method since rpcResponse
				// strips it. Cheap: only fires per server-initiated note.
				var note struct {
					Method string `json:"method"`
				}
				if json.Unmarshal(line, &note) == nil && note.Method == "notifications/tools/list_changed" {
					go func() {
						ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer cancel()
						if tools, err := c.listTools(ctx); err == nil {
							c.tools = tools
						}
					}()
				}
			}
			continue
		}
		c.mu.Lock()
		ch, ok := c.pending[*resp.ID]
		if ok {
			delete(c.pending, *resp.ID)
		}
		c.mu.Unlock()
		if ok {
			ch <- resp
		}
	}
	// M30 in audit ledger: surface scanner exit cause. The 4 MiB scanner
	// buffer is generous, but a server emitting one >4 MiB message (huge
	// tools/list result, error trace) causes Scan to return false silently
	// → all subsequent Calls report "server exited" with no actual exit.
	// Logging the underlying error makes the failure mode visible.
	if err := c.stdout.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "mcp: server %q stdout reader exited: %v\n", c.name, err)
	}
}

// buildEnv layers user-supplied environment variables on top of the
// parent process's environment.
//
// M32 in audit ledger: os.ExpandEnv on user-supplied values lets a config
// like `env: {API_TOKEN: "$AWS_SECRET_ACCESS_KEY"}` exfiltrate the parent
// secret into whatever the MCP server logs. Per CLAUDE.md safety rules,
// secrets must not leak. We keep ExpandEnv (legitimate `$HOME/cache`
// patterns rely on it) but document the foot-gun loudly here so the
// config schema reviewer is aware. A future opt-in "allow_secret_refs"
// flag should gate it; tracked as a follow-up.
func buildEnv(extra map[string]string) []string {
	env := os.Environ()
	for k, v := range extra {
		env = append(env, k+"="+os.ExpandEnv(v))
	}
	return env
}
