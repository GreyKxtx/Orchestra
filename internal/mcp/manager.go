package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/llm"
)

// Manager starts and manages multiple MCP server connections.
type Manager struct {
	clients []*Client
}

// NewManager starts all enabled MCP servers from the config.
// Non-fatal errors (individual server startup failures) are logged but don't abort.
func NewManager(ctx context.Context, cfg config.MCPConfig) (*Manager, []error) {
	m := &Manager{}
	var errs []error
	for _, srv := range cfg.Servers {
		if srv.Disabled || len(srv.Command) == 0 {
			continue
		}
		c, err := Start(ctx, srv.Name, srv.Command, srv.Env)
		if err != nil {
			errs = append(errs, fmt.Errorf("mcp server %q: %w", srv.Name, err))
			continue
		}
		m.clients = append(m.clients, c)
	}
	return m, errs
}

// IsEmpty reports whether there are no active MCP server connections.
func (m *Manager) IsEmpty() bool {
	return m == nil || len(m.clients) == 0
}

// Close stops all MCP server subprocesses.
func (m *Manager) Close() {
	if m == nil {
		return
	}
	for _, c := range m.clients {
		_ = c.Close()
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
