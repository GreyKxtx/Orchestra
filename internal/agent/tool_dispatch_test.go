package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol"
)

func TestNormalizeToolName(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"read", "read"},
		{"fs.read", "read"},
		{"TodoWrite", "todowrite"},
		{"task.result", "task_result"},
		{"Task", "task"},
		{"exec.run", "bash"},
		{"search.text", "grep"},
		{"mcp:server:tool", "mcp:server:tool"},
	}
	for _, tc := range tests {
		if got := normalizeToolName(tc.in); got != tc.want {
			t.Errorf("normalizeToolName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestAllParallelSafeCalls_falseWhenTodoreadInBatch(t *testing.T) {
	defs := tools.ListTools(tools.Capabilities{})
	calls := []ToolCall{
		{Name: "todoread"},
		{Name: "read", Input: []byte(`{"path":"a.txt"}`)},
	}
	if allParallelSafeCalls(calls, defs) {
		t.Fatal("expected false when batch contains todoread (in-process tool)")
	}
}

func TestAllParallelSafeCalls_trueForReadOnlyBatch(t *testing.T) {
	defs := tools.ListTools(tools.Capabilities{})
	calls := []ToolCall{
		{Name: "read", Input: []byte(`{"path":"a.txt"}`)},
		{Name: "glob", Input: []byte(`{"pattern":"*.go"}`)},
	}
	if !allParallelSafeCalls(calls, defs) {
		t.Fatal("expected true for parallel-safe read-only batch")
	}
}

func TestAllParallelSafeCalls_falseWhenTaskInBatch(t *testing.T) {
	defs := tools.ListToolsWithSubtasks(tools.Capabilities{})
	calls := []ToolCall{
		{Name: "task", Input: []byte(`{"prompt":"explore"}`)},
		{Name: "read", Input: []byte(`{"path":"a.txt"}`)},
	}
	if allParallelSafeCalls(calls, defs) {
		t.Fatal("expected false when batch contains task (in-process tool)")
	}
}

func TestNormalizeLLMWithDefs_mixedBatchReturnsAllTools(t *testing.T) {
	resp := &llm.CompleteResponse{
		Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{
				{ID: "c1", Type: "function", Function: llm.ToolCallFunc{Name: "read", Arguments: llm.ToolArguments([]byte(`{"path":"a.go"}`))}},
				{ID: "c2", Type: "function", Function: llm.ToolCallFunc{Name: "edit", Arguments: llm.ToolArguments([]byte(`{"path":"a.go","old_string":"x","new_string":"y"}`))}},
			},
		},
	}
	step, _, err := NormalizeLLMWithDefs(nil, resp, nil)
	if err != nil {
		t.Fatalf("NormalizeLLMWithDefs: %v", err)
	}
	if step.Type != StepToolCall {
		t.Fatalf("type = %q, want tool_call", step.Type)
	}
	if len(step.Tools) != 2 {
		t.Fatalf("len(Tools) = %d, want 2", len(step.Tools))
	}
	if step.Tool != nil {
		t.Fatal("expected Step.Tool nil for multi-call response")
	}
	if step.Tools[0].Name != "read" || step.Tools[1].Name != "edit" {
		t.Fatalf("tools = %+v", step.Tools)
	}
}

func TestFormatToolErrorJSON_KeepsResolverDetails(t *testing.T) {
	err := protocol.NewError(protocol.StaleContent, "search block not found", map[string]any{
		"path":    "pkg/a.go",
		"nearest": "file has 12 lines; current text around line 5:\n     5| func Greet(name string) string {",
		"empty":   "",
	})
	out := formatToolErrorJSON("edit", json.RawMessage(`{"path":"pkg/a.go"}`), err)

	var got map[string]any
	if e := json.Unmarshal([]byte(out), &got); e != nil {
		t.Fatalf("unmarshal: %v (%s)", e, out)
	}
	details, ok := got["details"].(map[string]any)
	if !ok {
		t.Fatalf("details dropped — the model cannot act on the error: %s", out)
	}
	if s, _ := details["nearest"].(string); !strings.Contains(s, "func Greet") {
		t.Fatalf("nearest region missing: %#v", details)
	}
	if _, present := details["empty"]; present {
		t.Fatalf("empty detail values should be dropped: %#v", details)
	}
}

func TestCompactErrorDetails_ClipsLongValues(t *testing.T) {
	long := strings.Repeat("x", toolErrorDetailMaxChars*2)
	out, _ := compactErrorDetails(map[string]any{"nearest": long}).(map[string]any)
	s, _ := out["nearest"].(string)
	if len(s) > toolErrorDetailMaxChars+16 {
		t.Fatalf("detail not clipped: %d chars", len(s))
	}
}
