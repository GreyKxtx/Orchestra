package sessionfile

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/llm"
)

func userAssistantHistory() []llm.Message {
	return []llm.Message{
		{Role: llm.RoleUser, Content: "u1"},
		{Role: llm.RoleAssistant, Content: "a1"},
		{Role: llm.RoleUser, Content: "u2"},
		{Role: llm.RoleAssistant, Content: "a2"},
		{Role: llm.RoleUser, Content: "u3"},
	}
}

func TestIndexOfNthUserMessage_FindsNth(t *testing.T) {
	hist := userAssistantHistory()
	for n, want := range map[int]int{1: 0, 2: 2, 3: 4} {
		if got := IndexOfNthUserMessage(hist, n); got != want {
			t.Errorf("IndexOfNthUserMessage(hist, %d) = %d, want %d", n, got, want)
		}
	}
}

func TestIndexOfNthUserMessage_MissingReturnsMinusOne(t *testing.T) {
	// Asking for more user messages than exist is exactly the post-compaction
	// case: fork must be able to detect it rather than cut at a wrong place.
	if got := IndexOfNthUserMessage(userAssistantHistory(), 4); got != -1 {
		t.Fatalf("got %d, want -1", got)
	}
	if got := IndexOfNthUserMessage(nil, 1); got != -1 {
		t.Fatalf("empty history: got %d, want -1", got)
	}
}

func TestIndexOfNthUserMessage_RejectsNonPositiveN(t *testing.T) {
	for _, n := range []int{0, -1} {
		if got := IndexOfNthUserMessage(userAssistantHistory(), n); got != -1 {
			t.Errorf("n=%d: got %d, want -1", n, got)
		}
	}
}

// A rewritten turn is MARKED, not deleted, so the array stays positionally
// aligned with the UI's user turns. TurnStartAt must therefore refuse exactly
// the marked turn and keep answering for its neighbours — the property that
// makes a session survive a cancelled turn instead of losing fork forever.
func TestTurnStartAt_RefusesTheUnknownTurnAndKeepsTheOthersUsable(t *testing.T) {
	starts := []int{0, TurnStartUnknown, 7}

	if _, err := TurnStartAt(starts, 1, 10); err == nil {
		t.Fatal("a turn whose boundary is unknown must be refused, not cut at")
	} else {
		msg := err.Error()
		if !strings.Contains(msg, "turn 2") {
			t.Errorf("the error must name the turn whose boundary is unknown, got: %v", err)
		}
		if !strings.Contains(msg, "unknown") || !strings.Contains(msg, "rewritten") {
			t.Errorf("the error must say the boundary is unknown because history was rewritten, got: %v", err)
		}
	}

	if got, err := TurnStartAt(starts, 0, 10); err != nil || got != 0 {
		t.Errorf("turn 1 = (%d, %v), want (0, nil) — one unknown turn must not disable the others", got, err)
	}
	if got, err := TurnStartAt(starts, 2, 10); err != nil || got != 7 {
		t.Errorf("turn 3 = (%d, %v), want (7, nil) — a turn recorded after the rewrite is usable again", got, err)
	}
}

// The pre-sentinel guarantees still hold: a short array (a session written
// before this turn was recorded) and an index outside the current history are
// still refused, and neither may be mistaken for the sentinel.
func TestTurnStartAt_StillRefusesShortArraysAndOutOfRangeEntries(t *testing.T) {
	if _, err := TurnStartAt([]int{0}, 2, 10); err == nil {
		t.Error("a turn past the end of the array must be refused")
	} else if !strings.Contains(err.Error(), "turn boundaries") {
		t.Errorf("error should say the session has no recorded turn boundaries for that turn, got: %v", err)
	}
	if _, err := TurnStartAt([]int{0, 99}, 1, 10); err == nil {
		t.Error("a boundary past the end of history must be refused")
	}
	if _, err := TurnStartAt([]int{0, -2}, 1, 10); err == nil {
		t.Error("a negative boundary that is not the sentinel must still be refused")
	}
	if _, err := TurnStartAt([]int{0}, -1, 10); err == nil {
		t.Error("a negative turn index must be refused")
	}
}

// MarkTurnStartsUnknown must never shorten the array: the length IS the
// alignment with the UI's user turns, and losing it is what made a single
// cancelled turn end forking for a whole session.
func TestMarkTurnStartsUnknown_PreservesLengthAndDoesNotAliasTheInput(t *testing.T) {
	in := []int{0, 3, 7}
	got := MarkTurnStartsUnknown(in)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 — marking must preserve positional alignment, not shorten the array", len(got))
	}
	for i, v := range got {
		if v != TurnStartUnknown {
			t.Fatalf("entry %d = %d, want the unknown sentinel", i, v)
		}
	}
	if in[0] != 0 || in[1] != 3 || in[2] != 7 {
		t.Fatalf("the input array was mutated: %v", in)
	}
	if got := MarkTurnStartsUnknown(nil); len(got) != 0 {
		t.Fatalf("nil input: got %v, want empty", got)
	}
}

func TestCountUserMessages(t *testing.T) {
	ui := []UIMessage{
		{Role: "user"}, {Role: "assistant"}, {Role: "system"}, {Role: "user"},
	}
	if got := CountUserMessages(ui); got != 2 {
		t.Fatalf("got %d, want 2", got)
	}
	if got := CountUserMessages(nil); got != 0 {
		t.Fatalf("nil: got %d, want 0", got)
	}
}
