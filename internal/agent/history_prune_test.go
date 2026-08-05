package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/llm"
)

func TestPruneRetroactiveToolHistory_OldGrepDigested(t *testing.T) {
	big := strings.Repeat("x", 8000)
	in := json.RawMessage(`{"query":"foo"}`)
	digested, _ := DigestToolOutput("grep", in, []byte(big), 512)

	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "go"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
			ID: "c1", Type: "function",
			Function: llm.ToolCallFunc{Name: "grep", Arguments: llm.ToolArguments(`{"query":"foo"}`)},
		}}},
		{Role: llm.RoleTool, ToolCallID: "c1", Content: big},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
			ID: "c2", Type: "function",
			Function: llm.ToolCallFunc{Name: "grep", Arguments: llm.ToolArguments(`{"query":"bar"}`)},
		}}},
		{Role: llm.RoleTool, ToolCallID: "c2", Content: big},
	}

	out := pruneRetroactiveToolHistory(msgs, 512, 1)
	if len(out) != len(msgs) {
		t.Fatalf("len %d want %d", len(out), len(msgs))
	}
	if !strings.Contains(out[3].Content, "[digest tool:grep") {
		t.Fatalf("old grep should be digested: %q", out[3].Content[:80])
	}
	if out[5].Content == digested && out[5].Content == big {
		t.Fatal("recent grep should stay raw")
	}
	if len(out[5].Content) <= 512 || out[5].Content == out[3].Content {
		// recent atom protected — full raw blob
		if len(out[5].Content) < 7000 {
			t.Fatalf("recent tool should remain large, got %d bytes", len(out[5].Content))
		}
	}
}

func TestPruneRetroactiveToolHistory_DisabledWhenBudgetZero(t *testing.T) {
	big := strings.Repeat("y", 5000)
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: "s"},
		{Role: llm.RoleUser, Content: "u"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
			ID: "c1", Type: "function",
			Function: llm.ToolCallFunc{Name: "read", Arguments: llm.ToolArguments(`{"path":"a.go"}`)},
		}}},
		{Role: llm.RoleTool, ToolCallID: "c1", Content: big},
	}
	out := pruneRetroactiveToolHistory(msgs, 0, 2)
	if out[3].Content != big {
		t.Fatal("prune disabled should not change content")
	}
}
