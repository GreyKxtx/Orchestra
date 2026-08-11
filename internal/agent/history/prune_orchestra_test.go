package history

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/llm"
)

func TestCollapseOrchestraWorkerTaskOutputs(t *testing.T) {
	bigResult := `{"task_id":"t1","status":"done","result":"` + strings.Repeat("x", 2000) + `"}`

	assistant := llm.Message{
		Role: llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{
			ID: "c1",
			Function: llm.ToolCallFunc{
				Name:      "task",
				Arguments: llm.ToolArguments([]byte(`{"subagent_type":"worker"}`)),
			},
		}},
	}
	toolOld := llm.Message{Role: llm.RoleTool, ToolCallID: "c1", Content: bigResult}
	assistant2 := llm.Message{
		Role: llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{
			ID: "c2",
			Function: llm.ToolCallFunc{Name: "read", Arguments: llm.ToolArguments([]byte(`{"path":"a.go"}`))},
		}},
	}
	toolRecent := llm.Message{Role: llm.RoleTool, ToolCallID: "c2", Content: `{"content":"full read stays"}`}

	prefix := []llm.Message{{Role: llm.RoleSystem, Content: "sys"}, {Role: llm.RoleUser, Content: "go"}}
	messages := append(append(prefix, assistant, toolOld), assistant2, toolRecent)

	compact := func(_ string, content string) (string, bool) {
		if strings.Contains(content, "task_id") {
			return `{"task_id":"t1","status":"done","result":"worker compact"}`, true
		}
		return "", false
	}

	out := CollapseOrchestraWorkerTaskOutputs(messages, 1, compact)
	if len(out) != len(messages) {
		t.Fatalf("expected same message count, got %d want %d", len(out), len(messages))
	}
	foundCompact := false
	for _, m := range out {
		if m.Role == llm.RoleTool && m.ToolCallID == "c1" {
			if !strings.Contains(m.Content, "worker compact") {
				t.Fatalf("old task not compacted: %q", m.Content)
			}
			foundCompact = true
		}
		if m.Role == llm.RoleTool && m.ToolCallID == "c2" {
			if !strings.Contains(m.Content, "full read stays") {
				t.Fatalf("recent tool should stay full: %q", m.Content)
			}
		}
	}
	if !foundCompact {
		t.Fatal("missing compacted worker task")
	}
}
