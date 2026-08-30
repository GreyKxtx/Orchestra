package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/llm"
)

func toolAtom(id, body string) []llm.Message {
	return []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: id, Type: "function",
			Function: llm.ToolCallFunc{
				Name:      "read",
				Arguments: llm.ToolArguments(json.RawMessage(`{"path":"a.go"}`)),
			}}}},
		{Role: llm.RoleTool, ToolCallID: id, Content: body},
	}
}

func TestSplitHistoryForCompaction_KeepsRecentTailVerbatim(t *testing.T) {
	hist := []llm.Message{{Role: llm.RoleUser, Content: "goal"}}
	for _, id := range []string{"a", "b", "c", "d", "e", "f"} {
		hist = append(hist, toolAtom(id, strings.Repeat(id, 400))...)
	}

	older, tail := splitHistoryForCompaction(hist, 1200, 2)
	if len(older) == 0 || len(tail) == 0 {
		t.Fatalf("split produced older=%d tail=%d", len(older), len(tail))
	}
	// The newest atom must survive verbatim.
	if !strings.Contains(tail[len(tail)-1].Content, "ff") {
		t.Fatalf("newest atom not in tail: %q", tail[len(tail)-1].Content)
	}
	// The oldest content must be on the summarise side.
	if !strings.Contains(older[0].Content, "goal") {
		t.Fatalf("oldest message not in older: %+v", older[0])
	}
	// Tail must stay a minority of the history so compaction actually shrinks it.
	if historyBytes(tail)*2 > historyBytes(hist) {
		t.Fatalf("tail too large: %d of %d bytes", historyBytes(tail), historyBytes(hist))
	}
	// Atom integrity: every tool reply in the tail keeps its assistant call.
	seen := map[string]bool{}
	for _, m := range tail {
		for _, tc := range m.ToolCalls {
			seen[tc.ID] = true
		}
		if m.Role == llm.RoleTool && !seen[m.ToolCallID] {
			t.Fatalf("orphaned tool reply %q in tail", m.ToolCallID)
		}
	}
}

func TestSplitHistoryForCompaction_MinToolAtomsBeatsTinyBudget(t *testing.T) {
	hist := []llm.Message{{Role: llm.RoleUser, Content: "goal"}}
	for _, id := range []string{"a", "b", "c", "d"} {
		hist = append(hist, toolAtom(id, strings.Repeat(id, 200))...)
	}
	_, tail := splitHistoryForCompaction(hist, 1, 2)
	tools := 0
	for _, m := range tail {
		if m.Role == llm.RoleTool {
			tools++
		}
	}
	if tools < 2 {
		t.Fatalf("tail kept %d tool replies, want >= 2 even on a 1-byte budget", tools)
	}
}

func TestSplitHistoryForCompaction_NothingOlderThanTail(t *testing.T) {
	hist := toolAtom("a", "short")
	older, tail := splitHistoryForCompaction(hist, 64*1024, 2)
	if len(older) != len(hist) || tail != nil {
		t.Fatalf("single atom must stay summarisable: older=%d tail=%d", len(older), len(tail))
	}
}
