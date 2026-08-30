package history

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/llm"
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
	got := TruncateMessages(msgs, 1024)

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
	got := TruncateMessages(msgs, 1<<20) // 1 MB budget — fits everything
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
	got := TruncateMessages(msgs, 512)
	for _, m := range got {
		if m.Role == llm.RoleAssistant && len(m.ToolCalls) > 0 {
			t.Fatalf("dangling assistant with tool_calls survived truncation: %+v", got)
		}
	}
}

// TestTruncateMessages_StripsOrphanedToolCalls is the N4 regression
// (Sprint 6 in audit ledger). When the input history contains an
// assistant opening N tool_calls but only M<N replies arrived (partial
// API response, mid-batch error), truncation must filter the orphan IDs
// out of the assistant's ToolCalls so the surviving history is self-
// consistent. Without sanitize, every subsequent LLM call fails with
// "tool_call_id not found" and the run dies.
func TestTruncateMessages_StripsOrphanedToolCalls(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "init"},
		// Assistant opens THREE tool_calls.
		{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{
				{ID: "A", Type: "function", Function: llm.ToolCallFunc{Name: "read"}},
				{ID: "B", Type: "function", Function: llm.ToolCallFunc{Name: "read"}},
				{ID: "C", Type: "function", Function: llm.ToolCallFunc{Name: "read"}},
			},
		},
		// Only TWO replies arrive — C is orphaned.
		{Role: llm.RoleTool, ToolCallID: "A", Content: "okA"},
		{Role: llm.RoleTool, ToolCallID: "B", Content: "okB"},
		// A later, unrelated assistant turn closes the orphan atom.
		{Role: llm.RoleUser, Content: "follow up"},
	}
	got := TruncateMessages(msgs, 100_000) // huge budget — no eviction by size

	var sawAssistant bool
	for _, m := range got {
		if m.Role != llm.RoleAssistant || len(m.ToolCalls) == 0 {
			continue
		}
		sawAssistant = true
		for _, tc := range m.ToolCalls {
			if tc.ID == "C" {
				t.Fatalf("orphan tool_call %q survived sanitize: %+v", tc.ID, m.ToolCalls)
			}
		}
		if len(m.ToolCalls) != 2 {
			t.Fatalf("expected 2 surviving tool_calls (A, B), got %d: %+v", len(m.ToolCalls), m.ToolCalls)
		}
	}
	if !sawAssistant {
		t.Fatal("assistant message was wrongly dropped (it still has valid tool_calls)")
	}
}

// TestTruncateMessages_DropsAssistantWhenAllToolCallsOrphaned —
// companion case to the above: if ALL of an assistant's tool_calls
// are orphans AND it has no other content, drop the assistant message
// entirely so we don't emit an empty turn.
func TestTruncateMessages_DropsAssistantWhenAllToolCallsOrphaned(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "init"},
		// Assistant with two tool_calls, both orphans, no text.
		{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{
				{ID: "X", Type: "function", Function: llm.ToolCallFunc{Name: "read"}},
				{ID: "Y", Type: "function", Function: llm.ToolCallFunc{Name: "read"}},
			},
		},
		// No tool replies — both orphaned. Followed by a user message that closes the atom.
		{Role: llm.RoleUser, Content: "follow up"},
	}
	got := TruncateMessages(msgs, 100_000)

	for _, m := range got {
		if m.Role == llm.RoleAssistant && len(m.ToolCalls) > 0 {
			t.Fatalf("empty-content assistant with all-orphan tool_calls survived: %+v", m)
		}
	}
}

func TestTruncateMessages_MarksTheGap(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "goal"},
	}
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		msgs = append(msgs,
			llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
				ID:   id,
				Type: "function",
				Function: llm.ToolCallFunc{
					Name:      "read",
					Arguments: llm.ToolArguments(json.RawMessage(`{"path":"pkg/` + id + `.go"}`)),
				},
			}}},
			llm.Message{Role: llm.RoleTool, ToolCallID: id, Content: strings.Repeat(id, 500)},
		)
	}

	out := TruncateMessages(msgs, 2500)
	if len(out) >= len(msgs) {
		t.Fatalf("nothing was truncated: %d messages", len(out))
	}
	if out[2].Role != llm.RoleUser || !strings.HasPrefix(out[2].Content, GapMarkerPrefix) {
		t.Fatalf("no gap marker after the required head: %+v", out[2])
	}
	if !strings.Contains(out[2].Content, "pkg/a.go") {
		t.Fatalf("marker does not name the dropped files: %q", out[2].Content)
	}

	// Truncating again must not stack markers: the old one is dropped with the
	// atoms it described and replaced by a fresh one.
	again := TruncateMessages(out, 2000)
	markers := 0
	for _, m := range again {
		if strings.HasPrefix(m.Content, GapMarkerPrefix) {
			markers++
		}
	}
	if markers > 1 {
		t.Fatalf("gap markers accumulated: %d", markers)
	}
}

func TestTruncateMessages_NoMarkerWhenNothingDropped(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "goal"},
		{Role: llm.RoleAssistant, Content: "ok"},
	}
	out := TruncateMessages(msgs, 64*1024)
	for _, m := range out {
		if strings.HasPrefix(m.Content, GapMarkerPrefix) {
			t.Fatal("marker added although the history fit")
		}
	}
}
