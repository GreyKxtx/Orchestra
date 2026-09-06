package sessionfile

import (
	"fmt"

	"github.com/orchestra/orchestra/llm"
)

// TurnStartAt returns the History index at which the (k+1)-th user turn's
// agent output begins, given a session's recorded boundaries and its history
// length. It is the single place fork and rewind agree on where a turn starts.
//
// It fails rather than guess. A session has no boundary for turn k+1 when it
// was written before boundaries were recorded, or when /compact rewrote its
// history wholesale (SessionCompact clears the field, because every recorded
// index points into the pre-compaction array). A stored index outside the
// current history means the two went out of sync; cutting there would produce
// a branch nobody asked for.
func TurnStartAt(turnStarts []int, k, histLen int) (int, error) {
	if k < 0 {
		return 0, fmt.Errorf("turn index %d is negative", k)
	}
	if len(turnStarts) <= k {
		return 0, fmt.Errorf("this session has no recorded turn boundaries for turn %d "+
			"(it has %d of them): it was recorded before turn boundaries were tracked, "+
			"or its history was rewritten by /compact", k+1, len(turnStarts))
	}
	cut := turnStarts[k]
	if cut < 0 || cut > histLen {
		return 0, fmt.Errorf("recorded turn boundary %d for turn %d is outside a history of %d messages",
			cut, k+1, histLen)
	}
	return cut, nil
}

// IndexOfNthUserMessage returns the position of the nth (1-based) user message
// in hist, or -1 when hist holds fewer than n user messages.
//
// It returns a position rather than a slice because the two callers cut on
// opposite sides of it: rewind keeps the message it lands on, fork drops it.
func IndexOfNthUserMessage(hist []llm.Message, n int) int {
	if n <= 0 {
		return -1
	}
	seen := 0
	for i, m := range hist {
		if m.Role != llm.RoleUser {
			continue
		}
		seen++
		if seen == n {
			return i
		}
	}
	return -1
}

// CountUserMessages reports how many user messages a UI prefix holds. The UI
// projection and the LLM history are separate position-indexed arrays with no
// stable per-message id, so counting user turns is the only way to map one
// onto the other.
func CountUserMessages(ui []UIMessage) int {
	n := 0
	for _, m := range ui {
		if m.Role == "user" {
			n++
		}
	}
	return n
}
