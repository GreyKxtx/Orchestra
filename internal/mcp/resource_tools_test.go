package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func toolNames(m *Manager) []string {
	var out []string
	for _, d := range m.ListToolDefs() {
		out = append(out, d.Function.Name)
	}
	return out
}

func has(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestResourceTools_OfferedOnlyByServersThatHaveResources(t *testing.T) {
	withRes := newResourceManager()
	names := toolNames(withRes)
	for _, want := range []string{"mcp:docs:resources_list", "mcp:docs:resources_read"} {
		if !has(names, want) {
			t.Errorf("missing %q in %v", want, names)
		}
	}

	// A server with no resources must not advertise the pair — two dead tools
	// per server is a real cost in the tool list every request carries.
	plain := &Manager{clients: []ServerClient{&richStub{}}}
	for _, n := range toolNames(plain) {
		if strings.Contains(n, "resources_") {
			t.Errorf("plain server advertised %q", n)
		}
	}
}

func TestResourceTools_ListReturnsTheServersResources(t *testing.T) {
	m := newResourceManager()
	out, err := m.Call(context.Background(), "mcp:docs:resources_list", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Resources []MCPResource `json:"resources"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal %s: %v", out, err)
	}
	if len(got.Resources) != 2 || got.Resources[0].URI != "file:///notes.md" {
		t.Fatalf("resources = %+v", got.Resources)
	}
}

func TestResourceTools_ReadReturnsTheBody(t *testing.T) {
	m := newResourceManager()
	out, err := m.Call(context.Background(), "mcp:docs:resources_read", json.RawMessage(`{"uri":"file:///notes.md"}`))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Result, "the body") {
		t.Errorf("result = %q", got.Result)
	}
}

func TestResourceTools_ReadWithoutURIExplainsItself(t *testing.T) {
	m := newResourceManager()
	_, err := m.Call(context.Background(), "mcp:docs:resources_read", json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "uri") {
		t.Fatalf("err = %v, want it to name the missing argument", err)
	}
}

func TestResourceTools_DoNotShadowARealToolOfTheSameName(t *testing.T) {
	// If a server genuinely exposes a tool called resources_read, that tool
	// wins: synthesising over it would silently change what the model calls.
	m := &Manager{clients: []ServerClient{&resourceStubWithTool{
		resourceStub: resourceStub{name: "docs", body: "x"},
	}}}
	out, err := m.Call(context.Background(), "mcp:docs:resources_read", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "real tool") {
		t.Errorf("out = %s, want the server's own tool to answer", out)
	}
}

// resourceStubWithTool serves resources *and* has its own resources_read tool.
type resourceStubWithTool struct{ resourceStub }

func (r *resourceStubWithTool) Tools() []MCPTool {
	return []MCPTool{{Name: "resources_read", Description: "the server's own"}}
}
func (r *resourceStubWithTool) Call(ctx context.Context, tool string, args json.RawMessage) (string, error) {
	return "real tool", nil
}
func (r *resourceStubWithTool) CallRich(ctx context.Context, tool string, args json.RawMessage) (string, []MCPImage, error) {
	return "real tool", nil, nil
}

// countingResourceStub records how many times the server was asked to list.
type countingResourceStub struct {
	resourceStub
	listCalls int
}

func (c *countingResourceStub) ListResources(ctx context.Context) ([]MCPResource, error) {
	c.listCalls++
	return c.res, nil
}

func TestResourceTools_ListedOnceNotOnEveryRequest(t *testing.T) {
	// ListToolDefs runs on every agent step. A round-trip to each MCP server
	// per step would add real latency to every single LLM call.
	stub := &countingResourceStub{resourceStub: resourceStub{
		name: "docs",
		res:  []MCPResource{{URI: "file:///notes.md", Name: "Notes"}},
	}}
	m := &Manager{clients: []ServerClient{stub}}

	for i := 0; i < 5; i++ {
		m.ListToolDefs()
	}
	if stub.listCalls != 1 {
		t.Fatalf("resources/list called %d times across 5 steps, want 1", stub.listCalls)
	}
}
