package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/orchestra/orchestra/llm"
)

// MCPResource is one resource a server offers as context — a file, a page, a
// query result. Unlike a tool, reading it has no side effects.
type MCPResource struct {
	Server      string `json:"server"`
	URI         string `json:"uri"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	MIMEType    string `json:"mime_type,omitempty"`
}

// resourceServer is the optional resources half of a ServerClient.
// resources/* is optional in MCP, so a server that does not implement it must
// not break listing for the servers that do.
type resourceServer interface {
	ListResources(ctx context.Context) ([]MCPResource, error)
	ReadResource(ctx context.Context, uri string) (string, error)
}

// ListResources returns every resource across all servers, each tagged with
// the server that offers it. Errors are reported to stderr rather than
// returned: one broken server must not hide the rest.
func (m *Manager) ListResources(ctx context.Context) []MCPResource {
	if m.IsEmpty() {
		return nil
	}
	var out []MCPResource
	for _, c := range m.clients {
		rs, ok := c.(resourceServer)
		if !ok {
			continue
		}
		items, err := rs.ListResources(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mcp: server %q resources/list: %v\n", c.ServerName(), err)
			continue
		}
		for _, it := range items {
			it.Server = c.ServerName()
			out = append(out, it)
		}
	}
	return out
}

// ReadResource fetches one resource's text from a named server.
func (m *Manager) ReadResource(ctx context.Context, server, uri string) (string, error) {
	c := m.findClient(strings.TrimSpace(server))
	if c == nil {
		return "", fmt.Errorf("mcp server %q not found", server)
	}
	rs, ok := c.(resourceServer)
	if !ok {
		return "", fmt.Errorf("mcp server %q does not serve resources", server)
	}
	return rs.ReadResource(ctx, strings.TrimSpace(uri))
}

// --- stdio transport ---

// ListResources implements resourceServer over stdio. A server that does not
// implement resources/* answers with a JSON-RPC error, which is reported as
// no resources rather than as a failure: the capability is optional.
func (c *Client) ListResources(ctx context.Context) ([]MCPResource, error) {
	raw, err := c.call(ctx, "resources/list", nil)
	if err != nil {
		if isMethodNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var res struct {
		Resources []MCPResource `json:"resources"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("resources/list: %w", err)
	}
	return res.Resources, nil
}

// ReadResource implements resourceServer over stdio.
func (c *Client) ReadResource(ctx context.Context, uri string) (string, error) {
	raw, err := c.call(ctx, "resources/read", map[string]any{"uri": uri})
	if err != nil {
		return "", err
	}
	var res struct {
		Contents []struct {
			URI      string `json:"uri"`
			MIMEType string `json:"mimeType"`
			Text     string `json:"text"`
			Blob     string `json:"blob"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", fmt.Errorf("resources/read %s: %w", uri, err)
	}
	var b strings.Builder
	binary := 0
	for _, ct := range res.Contents {
		if ct.Text != "" {
			b.WriteString(ct.Text)
			continue
		}
		if ct.Blob != "" {
			binary++
		}
	}
	if binary > 0 {
		// Same contract as tool results: name what could not be carried.
		fmt.Fprintf(&b, "\n[orchestra: %d binary resource part(s) omitted]", binary)
	}
	return b.String(), nil
}

// isMethodNotFound reports a JSON-RPC "method not found" (-32601), which is
// how a server declines an optional capability.
func isMethodNotFound(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "-32601") || strings.Contains(s, "method not found")
}

// Resource access is offered to the model as two synthetic tools per server
// that actually has resources — mcp:<server>:resources_list and
// mcp:<server>:resources_read. Synthesising tools rather than adding a new
// tool-registry entry keeps this inside the MCP package: the per-server
// allowlist, the restart path and the "mcp:" routing all apply unchanged, and
// no other package learns about resources.
const (
	resourceListTool = "resources_list"
	resourceReadTool = "resources_read"
)

// resourceToolDefs returns the synthetic defs for one server, or nil when it
// serves no resources. Two dead tools per server would be a real cost: the
// tool list travels with every single request.
func (m *Manager) resourceToolDefs(c ServerClient) []llm.ToolDef {
	if _, ok := c.(resourceServer); !ok {
		return nil
	}
	items := m.cachedResources(c)
	if len(items) == 0 {
		return nil
	}
	server := c.ServerName()
	// A server that genuinely exposes a tool by one of these names keeps it:
	// synthesising over it would silently change what the model calls.
	taken := map[string]bool{}
	for _, t := range c.Tools() {
		taken[t.Name] = true
	}
	var out []llm.ToolDef
	if !taken[resourceListTool] {
		out = append(out, llm.ToolDef{
			Type: "function",
			Function: llm.ToolFunctionDef{
				Name:        "mcp:" + server + ":" + resourceListTool,
				Description: "List the context resources offered by the " + server + " MCP server (uri, name, description). Reading a resource has no side effects.",
				Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
			},
		})
	}
	if !taken[resourceReadTool] {
		out = append(out, llm.ToolDef{
			Type: "function",
			Function: llm.ToolFunctionDef{
				Name:        "mcp:" + server + ":" + resourceReadTool,
				Description: "Read one resource from the " + server + " MCP server by uri (see " + resourceListTool + ").",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"uri":{"type":"string","description":"Resource URI as reported by ` + resourceListTool + `"}},"required":["uri"]}`),
			},
		})
	}
	return out
}

// callResourceTool handles the synthetic resource tools. ok is false when the
// name is not one of them, or when the server has a real tool by that name.
func (m *Manager) callResourceTool(ctx context.Context, c ServerClient, toolName string, input json.RawMessage) (json.RawMessage, bool, error) {
	if toolName != resourceListTool && toolName != resourceReadTool {
		return nil, false, nil
	}
	for _, t := range c.Tools() {
		if t.Name == toolName {
			return nil, false, nil // the server's own tool wins
		}
	}
	rs, ok := c.(resourceServer)
	if !ok {
		return nil, false, nil
	}

	if toolName == resourceListTool {
		items, err := rs.ListResources(ctx)
		if err != nil {
			return nil, true, err
		}
		for i := range items {
			items[i].Server = c.ServerName()
		}
		out, _ := json.Marshal(struct {
			Resources []MCPResource `json:"resources"`
		}{Resources: items})
		return out, true, nil
	}

	var args struct {
		URI string `json:"uri"`
	}
	_ = json.Unmarshal(input, &args)
	if strings.TrimSpace(args.URI) == "" {
		return nil, true, fmt.Errorf("mcp %s: uri is required — call %s first to see the available uris",
			resourceReadTool, resourceListTool)
	}
	body, err := rs.ReadResource(ctx, strings.TrimSpace(args.URI))
	if err != nil {
		return nil, true, err
	}
	out, _ := json.Marshal(mcpCallPayload{Result: body})
	return out, true, nil
}

// cachedResources returns a server's resource list, asking the server at most
// once per Manager lifetime.
//
// ListToolDefs runs on every agent step, so querying each server there would
// put a round-trip in front of every single LLM call. The cache is dropped
// when a server restarts (maybeRestart), which is also when its resource set
// can legitimately change.
func (m *Manager) cachedResources(c ServerClient) []MCPResource {
	name := c.ServerName()

	m.mu.Lock()
	if m.resCache != nil {
		if items, done := m.resCache[name]; done {
			m.mu.Unlock()
			return items
		}
	}
	m.mu.Unlock()

	rs, ok := c.(resourceServer)
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	items, err := rs.ListResources(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mcp: server %q resources/list: %v\n", name, err)
		items = nil // cache the miss too: a server that cannot list will not start
	}

	m.mu.Lock()
	if m.resCache == nil {
		m.resCache = map[string][]MCPResource{}
	}
	m.resCache[name] = items
	m.mu.Unlock()
	return items
}
