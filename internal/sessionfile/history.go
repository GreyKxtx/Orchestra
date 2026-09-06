package sessionfile

import "github.com/orchestra/orchestra/llm"

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
