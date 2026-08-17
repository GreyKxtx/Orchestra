package agent

import (
	"testing"

	promptpkg "github.com/orchestra/orchestra/internal/prompt"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
)

func TestOrchestraLeadStep1TokenBudget(t *testing.T) {
	defs := tools.ListToolsForMode("orchestra", tools.Capabilities{}, true, true)
	if len(defs) > 14 {
		t.Fatalf("Lead tools = %d, want ≤14", len(defs))
	}
	sys := promptpkg.BuildSystemPromptForMode("orchestra", "default")
	req := llm.CompleteRequest{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: sys},
			{Role: llm.RoleUser, Content: "test"},
		},
		Tools: defs,
	}
	tok := llm.EstimateCompleteRequestTokens(req)
	if tok > 8000 {
		t.Fatalf("step-1 estimate %d tokens exceeds 8000 (sys=%d bytes, tools=%d)", tok, len(sys), len(defs))
	}
}
