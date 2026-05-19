package agent

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/llm"
)

// TestTruncateMessages_KeepsAssistantToolGroupsTogether is the C1 regression:
// when the byte budget can't fit the most recent assistant↔tool atom whole,
// the truncator must drop the entire atom rather than keep an orphaned tool
// message. An orphan would make the next LLM call fail with "tool_call_id
// not found" and kill the run.
func TestTruncateMessages_KeepsAssistantToolGroupsTogether(t *testing.T) {
	bigText := strings.Repeat("x", 4000) // ~4 KB

	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "init"},
		// Atom A: small assistant call + small tool reply.
		{Role: llm.RoleAssistant, Content: "", ToolCalls: []llm.ToolCall{{ID: "a1", Type: "function", Function: llm.ToolCallFunc{Name: "read"}}}},
		{Role: llm.RoleTool, ToolCallID: "a1", Content: "ok"},
		// Atom B: huge assistant call + huge tool reply (won't fit budget).
		{Role: llm.RoleAssistant, Content: bigText, ToolCalls: []llm.ToolCall{{ID: "b1", Type: "function", Function: llm.ToolCallFunc{Name: "read"}}}},
		{Role: llm.RoleTool, ToolCallID: "b1", Content: bigText},
	}

	// Budget that covers system + initial user + atom A, but NOT atom B.
	got := truncateMessages(msgs, 1024)

	// Walk the result and assert every tool message has a preceding assistant
	// with a matching tool_call_id in the same atom — i.e. no orphans.
	openCalls := map[string]bool{}
	for _, m := range got {
		if m.Role == llm.RoleAssistant {
			openCalls = map[string]bool{}
			for _, tc := range m.ToolCalls {
				openCalls[tc.ID] = true
			}
			continue
		}
		if m.Role == llm.RoleTool {
			if !openCalls[m.ToolCallID] {
				t.Fatalf("orphaned tool message: ID=%q has no preceding assistant tool_call_id; result=%+v", m.ToolCallID, got)
			}
			delete(openCalls, m.ToolCallID)
		}
	}
}

// TestTruncateMessages_PreservesSmallTailAtoms confirms the happy path:
// when atoms fit, the most recent ones are kept whole.
func TestTruncateMessages_PreservesSmallTailAtoms(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "init"},
		{Role: llm.RoleAssistant, Content: "", ToolCalls: []llm.ToolCall{{ID: "1", Type: "function", Function: llm.ToolCallFunc{Name: "read"}}}},
		{Role: llm.RoleTool, ToolCallID: "1", Content: "r1"},
		{Role: llm.RoleAssistant, Content: "", ToolCalls: []llm.ToolCall{{ID: "2", Type: "function", Function: llm.ToolCallFunc{Name: "read"}}}},
		{Role: llm.RoleTool, ToolCallID: "2", Content: "r2"},
	}
	got := truncateMessages(msgs, 1<<20) // 1 MB budget — fits everything
	if len(got) != len(msgs) {
		t.Errorf("expected all %d messages kept, got %d", len(msgs), len(got))
	}
}

// TestTruncateMessages_AssistantWithToolCallsRequiresReplies guards the
// other half of the invariant: an assistant with `tool_calls` that has NO
// matching tool replies (e.g. they fell off the end) must also be evicted.
// Otherwise the next LLM call complains about missing tool result for
// declared tool_call_id.
func TestTruncateMessages_AssistantWithToolCallsRequiresReplies(t *testing.T) {
	bigText := strings.Repeat("y", 4000)
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "init"},
		// Atom: huge assistant with tool_calls but the tool reply (also huge)
		// is large enough that the whole atom can't fit a tiny budget.
		{Role: llm.RoleAssistant, Content: bigText, ToolCalls: []llm.ToolCall{{ID: "x", Type: "function", Function: llm.ToolCallFunc{Name: "read"}}}},
		{Role: llm.RoleTool, ToolCallID: "x", Content: bigText},
	}
	got := truncateMessages(msgs, 512)
	for _, m := range got {
		if m.Role == llm.RoleAssistant && len(m.ToolCalls) > 0 {
			t.Fatalf("dangling assistant with tool_calls survived truncation: %+v", got)
		}
	}
}
