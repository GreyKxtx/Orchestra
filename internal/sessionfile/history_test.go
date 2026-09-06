package sessionfile

import (
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
