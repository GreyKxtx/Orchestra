package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/llm"
)

// A small model will happily emit the same read a dozen times in ONE step.
// Every copy of the file then lands in the next prompt, which is the fastest
// way to blow a 16k context window. The batch runs the call once and points
// the duplicates at it.
func TestRunParallelToolBatch_IdenticalReadsRunOnce(t *testing.T) {
	ag, _ := newTestAgent(t, &scriptedLLM{steps: []string{`{"patches":[]}`}}, Options{})
	cb := NewCircuitBreaker(2, 6, 6, 3)
	input := []byte(`{"path":"a.txt"}`)
	calls := []ToolCall{
		{ID: "c1", Name: "read", Input: input},
		{ID: "c2", Name: "read", Input: input},
		{ID: "c3", Name: "read", Input: input},
		{ID: "c4", Name: "read", Input: input},
		{ID: "c5", Name: "read", Input: input},
		{ID: "c6", Name: "ls", Input: []byte(`{"path":"."}`)},
	}

	history, cbErr := ag.runParallelToolBatch(context.Background(), cb, nil, calls, nil, 1)
	if cbErr != nil {
		t.Fatalf("unexpected circuit-breaker trip: %v", cbErr)
	}
	// Tool replies only: the batch may also inject a user-role nudge about the
	// repeats, which is not an answer to a tool call.
	var replies []llm.Message
	for _, m := range history {
		if m.Role == llm.RoleTool {
			replies = append(replies, m)
		}
	}
	if len(replies) != 6 {
		t.Fatalf("every tool call must be answered: %d replies of %d messages", len(replies), len(history))
	}
	for i, m := range replies {
		if m.ToolCallID != calls[i].ID {
			t.Fatalf("reply %d is for %q, want %q", i, m.ToolCallID, calls[i].ID)
		}
	}
	history = replies
	if !strings.Contains(history[0].Content, "hello") {
		t.Fatalf("the first read must carry the file: %q", history[0].Content)
	}
	for _, i := range []int{1, 2, 3, 4} {
		if strings.Contains(history[i].Content, "hello") {
			t.Fatalf("duplicate %d repeated the file instead of pointing at it: %q", i, history[i].Content)
		}
		if !strings.Contains(history[i].Content, "identical") {
			t.Fatalf("duplicate %d must say what happened: %q", i, history[i].Content)
		}
	}
	if strings.Contains(history[5].Content, "identical") {
		t.Fatalf("a different call must still run: %q", history[3].Content)
	}

	// And the repeats still count, so the next step's identical call is
	// blocked rather than run a fourth time.
	if !cb.IsReadOnlyBlocked("read", input) {
		t.Fatal("the repeats in one batch must count towards the doom-loop guard")
	}
}
