package sessionfile

import (
	"fmt"

	"github.com/orchestra/orchestra/llm"
)

// TurnStartUnknown marks one turn whose boundary can no longer be known,
// because the history underneath it was rewritten wholesale after the
// boundary was recorded — /compact, the agent's mid-turn compaction, or the
// truncation fallback that drops entries off the front. The index that entry
// used to hold points into an array that no longer exists, and a
// bounds-checked cut at a stale index is worse than no cut at all: fork hands
// back a silently wrong branch and rewind, which is destructive and persisted,
// discards history a correct cut would have kept.
//
// It is a MARK and not a deletion because TurnStarts is positional: entry k
// describes user turn k+1, and fork and rewind reach it by counting user
// messages in the UI projection, which keeps growing. Dropping entries — which
// this code used to do by clearing the whole array — leaves it permanently
// shorter than that count, so every later lookup lands past the end and
// TurnStartAt refuses for the rest of the session: one cancelled turn ended
// forking for good. Marking keeps the array aligned, so only the turns that
// were actually rewritten under refuse, and turns recorded afterwards are
// forkable again.
//
// Every writer of TurnStarts must therefore preserve its length. Use
// MarkTurnStartsUnknown rather than reslicing or clearing.
const TurnStartUnknown = -1

// MarkTurnStartsUnknown returns a copy of starts with every entry replaced by
// TurnStartUnknown, preserving the array's length and therefore its alignment
// with the UI's user turns. Callers use it where a history rewrite invalidated
// the boundaries they hold; the input is left untouched.
func MarkTurnStartsUnknown(starts []int) []int {
	if len(starts) == 0 {
		return nil
	}
	out := make([]int, len(starts))
	for i := range out {
		out[i] = TurnStartUnknown
	}
	return out
}

// TurnStartAt returns the History index at which the (k+1)-th user turn's
// agent output begins, given a session's recorded boundaries and its history
// length. It is the single place fork and rewind agree on where a turn starts.
//
// It fails rather than guess, in three separable ways:
//
//   - No entry for turn k+1 at all: the array is shorter than the turn number,
//     because the session was written before boundaries were recorded, or was
//     recorded from partway through its life.
//   - The entry is TurnStartUnknown: history was rewritten under THAT turn, so
//     where it began is unknowable. Only that turn is refused — the others in
//     the same session stay usable, which is why the marker exists.
//   - The entry is outside the current history: the two went out of sync;
//     cutting there would produce a branch nobody asked for.
func TurnStartAt(turnStarts []int, k, histLen int) (int, error) {
	if k < 0 {
		return 0, fmt.Errorf("turn index %d is negative", k)
	}
	if len(turnStarts) <= k {
		return 0, fmt.Errorf("this session has no recorded turn boundaries for turn %d "+
			"(it has %d of them): it was recorded before turn boundaries were tracked, "+
			"or from partway through its life", k+1, len(turnStarts))
	}
	cut := turnStarts[k]
	if cut == TurnStartUnknown {
		return 0, fmt.Errorf("the boundary of turn %d is unknown: this session's history was "+
			"rewritten underneath it (by /compact, or by a turn that was interrupted after "+
			"compacting), so where that turn began can no longer be located — pick a turn "+
			"recorded after the rewrite", k+1)
	}
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
