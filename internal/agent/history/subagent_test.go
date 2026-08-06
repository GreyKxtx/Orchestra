package history

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/llm"
)

func TestFormatSubagentResult_ExploreFindings(t *testing.T) {
	big := strings.Repeat("x", 6000)
	hist := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "find Agent.Run"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
			ID: "c1", Type: "function",
			Function: llm.ToolCallFunc{Name: "explore", Arguments: llm.ToolArguments(`{"symbol_name":"Agent.Run"}`)},
		}}},
		{Role: llm.RoleTool, ToolCallID: "c1", Content: big},
	}
	out := FormatSubagentResult("explore", "find Agent.Run", hist, "main loop at 442", 512)
	if !strings.Contains(out, "[subagent:explore]") {
		t.Fatalf("missing header: %s", out)
	}
	if !strings.Contains(out, "## Findings") {
		t.Fatal("missing findings")
	}
	if !strings.Contains(out, "explore(Agent.Run)") {
		t.Fatal("missing explore finding")
	}
	if !strings.Contains(out, "## Result") {
		t.Fatal("missing result section")
	}
}
