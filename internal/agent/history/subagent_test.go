package history

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/llm"
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

func subagentToolAtom(id, tool, path, result string) []llm.Message {
	return []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
			ID:   id,
			Type: "function",
			Function: llm.ToolCallFunc{
				Name:      tool,
				Arguments: llm.ToolArguments(json.RawMessage(`{"path":"` + path + `"}`)),
			},
		}}},
		{Role: llm.RoleTool, ToolCallID: id, Content: result},
	}
}

func TestFormatSubagentFiles_ListsPathsAndTools(t *testing.T) {
	hist := []llm.Message{{Role: llm.RoleSystem, Content: "sys"}, {Role: llm.RoleUser, Content: "goal"}}
	hist = append(hist, subagentToolAtom("1", "read", "pkg/a.go", "content")...)
	hist = append(hist, subagentToolAtom("2", "edit", "pkg/a.go", "ok")...)
	hist = append(hist, subagentToolAtom("3", "read", "pkg/b.go", "content")...)

	out := FormatSubagentFiles(hist)
	if !strings.Contains(out, "pkg/a.go (read, edit)") {
		t.Fatalf("tools not collapsed per file:\n%s", out)
	}
	if !strings.Contains(out, "pkg/b.go (read)") {
		t.Fatalf("second file missing:\n%s", out)
	}
}

func TestFormatSubagentProgress_ReportsWhereItStopped(t *testing.T) {
	hist := []llm.Message{{Role: llm.RoleSystem, Content: "sys"}, {Role: llm.RoleUser, Content: "goal"}}
	hist = append(hist, subagentToolAtom("1", "read", "pkg/a.go", "content")...)
	hist = append(hist, llm.Message{Role: llm.RoleAssistant, Content: "about to patch the handler"})

	out := FormatSubagentProgress("worker", "fix the handler", hist, 1024)
	if !strings.Contains(out, "pkg/a.go") {
		t.Fatalf("files touched missing:\n%s", out)
	}
	if !strings.Contains(out, "about to patch the handler") {
		t.Fatalf("last activity missing:\n%s", out)
	}
}

func TestFormatSubagentProgress_EmptyHistory(t *testing.T) {
	if out := FormatSubagentProgress("worker", "goal", nil, 1024); out != "" {
		t.Fatalf("expected empty progress, got %q", out)
	}
}
