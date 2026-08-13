package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToolNameMapper_NilWhenAllValid(t *testing.T) {
	tools := []ToolDef{
		{Type: "function", Function: ToolFunctionDef{Name: "read"}},
		{Type: "function", Function: ToolFunctionDef{Name: "fs-write_2"}},
	}
	if m := newToolNameMapper(tools); m != nil {
		t.Fatalf("expected nil mapper for valid names, got %#v", m)
	}
}

func TestToolNameMapper_MCPRoundTrip(t *testing.T) {
	tools := []ToolDef{
		{Type: "function", Function: ToolFunctionDef{Name: "read"}},
		{Type: "function", Function: ToolFunctionDef{Name: "mcp:context7-mcp:resolve-library-id"}},
	}
	m := newToolNameMapper(tools)
	if m == nil {
		t.Fatal("expected mapper for mcp:* names")
	}
	wire := m.WireName("mcp:context7-mcp:resolve-library-id")
	if wire != "mcp_context7-mcp_resolve-library-id" {
		t.Fatalf("wire name = %q", wire)
	}
	if !wireToolNameValid(wire) {
		t.Fatalf("wire name %q still invalid", wire)
	}
	if got := m.Restore(wire); got != "mcp:context7-mcp:resolve-library-id" {
		t.Fatalf("restore = %q", got)
	}
	// Valid names pass through untouched.
	if got := m.WireName("read"); got != "read" {
		t.Fatalf("valid name mangled: %q", got)
	}
}

func TestToolNameMapper_CollisionGetsSuffix(t *testing.T) {
	tools := []ToolDef{
		{Type: "function", Function: ToolFunctionDef{Name: "mcp_srv_tool"}}, // already valid, reserves the name
		{Type: "function", Function: ToolFunctionDef{Name: "mcp:srv:tool"}}, // sanitizes to the same string
	}
	m := newToolNameMapper(tools)
	wire := m.WireName("mcp:srv:tool")
	if wire == "mcp_srv_tool" {
		t.Fatalf("collision not resolved: %q", wire)
	}
	if got := m.Restore(wire); got != "mcp:srv:tool" {
		t.Fatalf("restore = %q", got)
	}
	if got := m.Restore("mcp_srv_tool"); got != "mcp_srv_tool" {
		t.Fatalf("valid tool hijacked: %q", got)
	}
}

func TestToolNameMapper_WireMessagesRewritesHistory(t *testing.T) {
	tools := []ToolDef{
		{Type: "function", Function: ToolFunctionDef{Name: "mcp:memory:store"}},
	}
	m := newToolNameMapper(tools)
	msgs := []Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{
			{ID: "c1", Type: "function", Function: ToolCallFunc{Name: "mcp:memory:store"}},
		}},
	}
	out := m.WireMessages(msgs)
	if out[0].ToolCalls[0].Function.Name != "mcp_memory_store" {
		t.Fatalf("history not rewritten: %q", out[0].ToolCalls[0].Function.Name)
	}
	// Original history must stay untouched (it is replayed on later steps).
	if msgs[0].ToolCalls[0].Function.Name != "mcp:memory:store" {
		t.Fatalf("caller history mutated: %q", msgs[0].ToolCalls[0].Function.Name)
	}
}

func TestBuildChatBody_SanitizesMCPToolNames(t *testing.T) {
	c := &OpenAIClient{model: "test-model"}
	body, err := c.buildChatBody(CompleteRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools: []ToolDef{
			{Type: "function", Function: ToolFunctionDef{
				Name:       "mcp:context7-mcp:get-library-docs",
				Parameters: json.RawMessage(`{"type":"object"}`),
			}},
		},
	}, 100, false)
	if err != nil {
		t.Fatalf("buildChatBody: %v", err)
	}
	s := string(body)
	if strings.Contains(s, "mcp:context7-mcp") {
		t.Fatalf("wire body still contains ':' tool name: %s", s)
	}
	if !strings.Contains(s, `"name":"mcp_context7-mcp_get-library-docs"`) {
		t.Fatalf("sanitized name missing: %s", s)
	}
}
