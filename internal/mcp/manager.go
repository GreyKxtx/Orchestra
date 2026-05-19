package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/llm"
)

// Manager starts and manages multiple MCP server connections.
type Manager struct {
	clients []*Client
}

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
		c   *Client
		err error
	}
	var enabled []config.MCPServerConfig
	for _, srv := range cfg.Servers {
		if srv.Disabled || len(srv.Command) == 0 {
			continue
		}
		enabled = append(enabled, srv)
	}
	if len(enabled) == 0 {
		return m, nil
	}

	results := make([]startRes, len(enabled))
	var wg sync.WaitGroup
	for i, srv := range enabled {
		wg.Add(1)
		go func(idx int, srv config.MCPServerConfig) {
			defer wg.Done()
			startCtx, cancel := context.WithTimeout(ctx, mcpStartTimeout)
			defer cancel()
			c, err := Start(startCtx, srv.Name, srv.Command, srv.Env)
			results[idx] = startRes{idx: idx, c: c, err: err}
		}(i, srv)
	}
	wg.Wait()

	var errs []error
	for i, r := range results {
		if r.err != nil {
			errs = append(errs, fmt.Errorf("mcp server %q: %w", enabled[i].Name, r.err))
			continue
		}
		m.clients = append(m.clients, r.c)
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
func (m *Manager) Call(ctx context.Context, prefixedName string, input json.RawMessage) (json.RawMessage, error) {
	serverName, toolName, err := parseMCPToolName(prefixedName)
	if err != nil {
		return nil, err
	}
	c := m.findClient(serverName)
	if c == nil {
		return nil, fmt.Errorf("mcp server %q not found", serverName)
	}
	result, err := c.Call(ctx, toolName, input)
	if err != nil {
		return nil, err
	}
	out, _ := json.Marshal(map[string]string{"result": result})
	return out, nil
}

// IsMCPTool reports whether a tool name is an MCP-prefixed tool.
func IsMCPTool(name string) bool {
	return strings.HasPrefix(name, "mcp:")
}

func (m *Manager) findClient(serverName string) *Client {
	for _, c := range m.clients {
		if c.ServerName() == serverName {
			return c
		}
	}
	return nil
}

func parseMCPToolName(name string) (serverName, toolName string, err error) {
	// Format: "mcp:<server>:<tool>" — tool name may contain colons itself
	parts := strings.SplitN(name, ":", 3)
	if len(parts) != 3 || parts[0] != "mcp" {
		return "", "", fmt.Errorf("invalid mcp tool name %q (expected mcp:<server>:<tool>)", name)
	}
	return parts[1], parts[2], nil
}
