package history

import (
	"encoding/json"
	"github.com/orchestra/orchestra/internal/agent/digest"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/llm"
)

func TestPruneRetroactiveToolHistory_OldGrepDigested(t *testing.T) {
	big := strings.Repeat("x", 8000)
	in := json.RawMessage(`{"query":"foo"}`)
	digested, _ := digest.DigestToolOutput("grep", in, []byte(big), 512)

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

	out := PruneRetroactiveToolHistory(msgs, 512, 1)
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
	out := PruneRetroactiveToolHistory(msgs, 0, 2)
	if out[3].Content != big {
		t.Fatal("prune disabled should not change content")
	}
}

func TestPruneRetroactiveToolHistory_ProtectActivePath(t *testing.T) {
	big := strings.Repeat("z", 8000)
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "go"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
			ID: "c1", Type: "function",
			Function: llm.ToolCallFunc{Name: "read", Arguments: llm.ToolArguments(`{"path":"internal/llm/budget.go"}`)},
		}}},
		{Role: llm.RoleTool, ToolCallID: "c1", Content: big},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
			ID: "c2", Type: "function",
			Function: llm.ToolCallFunc{Name: "grep", Arguments: llm.ToolArguments(`{"query":"foo"}`)},
		}}},
		{Role: llm.RoleTool, ToolCallID: "c2", Content: big},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
			ID: "c3", Type: "function",
			Function: llm.ToolCallFunc{Name: "grep", Arguments: llm.ToolArguments(`{"query":"bar"}`)},
		}}},
		{Role: llm.RoleTool, ToolCallID: "c3", Content: big},
	}
	// keepRecent=1 protects only the last tool atom (c3); c1 would normally digest
	// unless protectPaths keeps budget.go.
	out := PruneRetroactiveToolHistory(msgs, 512, 1, "internal/llm/budget.go")
	if len(out[3].Content) < 7000 {
		t.Fatalf("protected read should stay full, got %d bytes", len(out[3].Content))
	}
	if !strings.Contains(out[5].Content, "[digest tool:grep") {
		t.Fatalf("unprotected old grep should be digested: %q", out[5].Content[:min(80, len(out[5].Content))])
	}
	if len(out[7].Content) < 7000 {
		t.Fatalf("recent grep should stay full, got %d", len(out[7].Content))
	}
}
