package state

import "strings"

// ReasoningSplitter routes streamed assistant text into a reasoning stream vs.
// a message stream based on `<think>` / `</think>` tags. State persists across
// Feed calls so a tag straddling two deltas is still recognized correctly.
//
// Provider note: qwen3, deepseek-r1, openai o1 et al. emit chain-of-thought
// inline with the final answer using these tags — keeping the split logic in
// state (not in the TUI App) makes it testable in isolation and lets future
// non-TUI clients (VS Code extension, headless) reuse it unchanged.
type ReasoningSplitter struct {
	// InThink is true while a `<think>` block is open. Exported so the TUI
	// can inspect it for "still thinking" UI cues and for terminal-flush logic
	// when the stream ends with carry leftover.
	InThink bool

	// Carry holds bytes from the previous delta that might be the prefix of a
	// `<think>` / `</think>` tag spanning two chunks. At most 7 bytes.
	Carry string
}

const (
	thinkOpen  = "<think>"
	thinkClose = "</think>"
)

// Feed processes one delta and returns the reasoning and message portions
// produced by this delta. Either may be empty.
func (s *ReasoningSplitter) Feed(delta string) (reasoning, message string) {
	buf := s.Carry + delta
	s.Carry = ""

	var rOut, mOut strings.Builder
	for len(buf) > 0 {
		if s.InThink {
			i := strings.Index(buf, thinkClose)
			if i < 0 {
				if hold := tagPrefixHold(buf, thinkClose); hold > 0 {
					rOut.WriteString(buf[:len(buf)-hold])
					s.Carry = buf[len(buf)-hold:]
				} else {
					rOut.WriteString(buf)
				}
				return rOut.String(), mOut.String()
			}
			rOut.WriteString(buf[:i])
			buf = buf[i+len(thinkClose):]
			s.InThink = false
			continue
		}

		i := strings.Index(buf, thinkOpen)
		if i < 0 {
			if hold := tagPrefixHold(buf, thinkOpen); hold > 0 {
				mOut.WriteString(buf[:len(buf)-hold])
				s.Carry = buf[len(buf)-hold:]
			} else {
				mOut.WriteString(buf)
			}
			return rOut.String(), mOut.String()
		}
		mOut.WriteString(buf[:i])
		buf = buf[i+len(thinkOpen):]
		s.InThink = true
	}
	return rOut.String(), mOut.String()
}

// Reset clears state — call at the start of a fresh assistant turn so leftover
// carry from a previous run doesn't leak.
func (s *ReasoningSplitter) Reset() {
	s.InThink = false
	s.Carry = ""
}

// tagPrefixHold returns N if the last N bytes of s could be the prefix of tag.
// e.g. s="hello </t" tag="</think>" → 3 (we should hold "</t").
// 0 means no risk — safe to emit s entirely.
func tagPrefixHold(s, tag string) int {
	maxN := len(tag) - 1
	if maxN > len(s) {
		maxN = len(s)
	}
	for n := maxN; n > 0; n-- {
		if strings.HasPrefix(tag, s[len(s)-n:]) {
			return n
		}
	}
	return 0
}
