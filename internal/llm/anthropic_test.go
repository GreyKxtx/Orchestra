package llm

import (
	"encoding/json"
	"testing"
)

func TestConvertToAnthropic_SystemExtracted(t *testing.T) {
	msgs := []Message{
		{Role: RoleSystem, Content: "be helpful"},
		{Role: RoleUser, Content: "hello"},
	}
	sys, out := convertToAnthropic(msgs)
	if sys != "be helpful" {
		t.Fatalf("expected system='be helpful', got %q", sys)
	}
	if len(out) != 1 || out[0].Role != "user" {
		t.Fatalf("expected 1 user message, got %v", out)
	}
}

func TestConvertToAnthropic_MultipleSystems(t *testing.T) {
	msgs := []Message{
		{Role: RoleSystem, Content: "part1"},
		{Role: RoleSystem, Content: "part2"},
		{Role: RoleUser, Content: "hi"},
	}
	sys, _ := convertToAnthropic(msgs)
	if sys != "part1\n\npart2" {
		t.Fatalf("expected joined system, got %q", sys)
	}
}

func TestConvertToAnthropic_AssistantWithToolCalls(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: "go"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{
			{
				ID:   "c1",
				Type: "function",
				Function: ToolCallFunc{
					Name:      "fs.read",
					Arguments: ToolArguments(json.RawMessage(`{"path":"a.txt"}`)),
				},
			},
		}},
	}
	_, out := convertToAnthropic(msgs)
	if len(out) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(out))
	}
	asst := out[1]
	if asst.Role != "assistant" {
		t.Fatalf("expected assistant role, got %q", asst.Role)
	}
	blocks, ok := asst.Content.([]anthropicBlock)
	if !ok || len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %v", asst.Content)
	}
	if blocks[0].Type != "tool_use" || blocks[0].Name != "fs.read" {
		t.Fatalf("unexpected block: %+v", blocks[0])
	}
}

func TestConvertToAnthropic_ToolResultsGrouped(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: "hi"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{
			{ID: "c1", Type: "function", Function: ToolCallFunc{Name: "fs.read", Arguments: ToolArguments(json.RawMessage(`{"path":"a.txt"}`))}},
			{ID: "c2", Type: "function", Function: ToolCallFunc{Name: "fs.read", Arguments: ToolArguments(json.RawMessage(`{"path":"b.txt"}`))}},
		}},
		{Role: RoleTool, ToolCallID: "c1", Content: "result1"},
		{Role: RoleTool, ToolCallID: "c2", Content: "result2"},
	}
	_, out := convertToAnthropic(msgs)
	// user, assistant, user (with 2 tool_result blocks grouped)
	if len(out) != 3 {
		t.Fatalf("expected 3 messages (user + assistant + grouped tool results), got %d: %+v", len(out), out)
	}
	last := out[2]
	if last.Role != "user" {
		t.Fatalf("expected last message role=user, got %q", last.Role)
	}
	blocks, ok := last.Content.([]anthropicBlock)
	if !ok {
		t.Fatalf("expected []anthropicBlock, got %T", last.Content)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 tool_result blocks, got %d", len(blocks))
	}
	for _, b := range blocks {
		if b.Type != "tool_result" {
			t.Fatalf("expected tool_result block, got type=%q", b.Type)
		}
	}
}

func TestConvertToAnthropic_ToolResultsNotGroupedAcrossAssistant(t *testing.T) {
	// Two separate tool rounds should produce separate user messages
	msgs := []Message{
		{Role: RoleUser, Content: "hi"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{
			{ID: "c1", Type: "function", Function: ToolCallFunc{Name: "fs.read", Arguments: ToolArguments(json.RawMessage(`{}`))}},
		}},
		{Role: RoleTool, ToolCallID: "c1", Content: "r1"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{
			{ID: "c2", Type: "function", Function: ToolCallFunc{Name: "fs.read", Arguments: ToolArguments(json.RawMessage(`{}`))}},
		}},
		{Role: RoleTool, ToolCallID: "c2", Content: "r2"},
	}
	_, out := convertToAnthropic(msgs)
	// user, asst, user(tool), asst, user(tool) = 5
	if len(out) != 5 {
		t.Fatalf("expected 5 messages, got %d: %+v", len(out), out)
	}
}

func TestConvertFromAnthropic_TextOnly(t *testing.T) {
	blocks := []anthropicBlock{
		{Type: "text", Text: "hello there"},
	}
	msg := convertFromAnthropic(blocks)
	if msg.Role != RoleAssistant {
		t.Fatalf("expected assistant role, got %q", msg.Role)
	}
	if msg.Content != "hello there" {
		t.Fatalf("expected 'hello there', got %q", msg.Content)
	}
	if len(msg.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(msg.ToolCalls))
	}
}

func TestConvertFromAnthropic_ToolUse(t *testing.T) {
	args := json.RawMessage(`{"path":"x.go"}`)
	blocks := []anthropicBlock{
		{Type: "tool_use", ID: "call_1", Name: "fs.read", Input: args},
	}
	msg := convertFromAnthropic(blocks)
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "call_1" {
		t.Fatalf("expected id=call_1, got %q", tc.ID)
	}
	if tc.Function.Name != "fs.read" {
		t.Fatalf("expected name=fs.read, got %q", tc.Function.Name)
	}
	if string(tc.Function.Arguments.Raw()) != `{"path":"x.go"}` {
		t.Fatalf("unexpected arguments: %s", tc.Function.Arguments.Raw())
	}
}

func TestConvertFromAnthropic_TextAndToolUse(t *testing.T) {
	blocks := []anthropicBlock{
		{Type: "text", Text: "I'll help"},
		{Type: "tool_use", ID: "c1", Name: "fs.list", Input: json.RawMessage(`{}`)},
	}
	msg := convertFromAnthropic(blocks)
	if msg.Content != "I'll help" {
		t.Fatalf("expected text content, got %q", msg.Content)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(msg.ToolCalls))
	}
}

func TestConvertTools_EmptyReturnsNil(t *testing.T) {
	if result := convertTools(nil); result != nil {
		t.Fatalf("expected nil for nil input, got %v", result)
	}
	if result := convertTools([]ToolDef{}); result != nil {
		t.Fatalf("expected nil for empty slice, got %v", result)
	}
}

func TestConvertTools_SetsDefaultSchema(t *testing.T) {
	defs := []ToolDef{
		{Type: "function", Function: ToolFunctionDef{Name: "test", Parameters: nil}},
	}
	out := convertTools(defs)
	if len(out) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(out))
	}
	if string(out[0].InputSchema) != `{"type":"object","properties":{}}` {
		t.Fatalf("unexpected default schema: %s", out[0].InputSchema)
	}
}

func TestConvertTools_PreservesExistingSchema(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)
	defs := []ToolDef{
		{Type: "function", Function: ToolFunctionDef{Name: "fs.read", Description: "reads", Parameters: schema}},
	}
	out := convertTools(defs)
	if len(out) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(out))
	}
	if out[0].Name != "fs.read" {
		t.Fatalf("expected name=fs.read, got %q", out[0].Name)
	}
	if out[0].Description != "reads" {
		t.Fatalf("expected description=reads, got %q", out[0].Description)
	}
	if string(out[0].InputSchema) != string(schema) {
		t.Fatalf("expected preserved schema, got %s", out[0].InputSchema)
	}
}
