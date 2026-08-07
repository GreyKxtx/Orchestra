package view

import (
	"fmt"

	"github.com/orchestra/orchestra/ui/tui/state"
)

const collapseOlderThan = 24 // keep last N messages fully expanded in the viewport list

// CollapseOldTurnsForView returns a display copy of messages where older turns
// are replaced by a single info line. Does not mutate the session SoT.
func CollapseOldTurnsForView(msgs []state.Message) []state.Message {
	if len(msgs) <= collapseOlderThan {
		out := make([]state.Message, len(msgs))
		copy(out, msgs)
		return out
	}
	hidden := len(msgs) - collapseOlderThan
	out := make([]state.Message, 0, collapseOlderThan+1)
	out = append(out, state.Message{
		Role:       state.RoleSystem,
		SystemKind: state.SystemKindInfo,
		Text:       fmt.Sprintf("⋯ %d earlier messages collapsed · scroll history in session file; LLM context may be compacted", hidden),
	})
	out = append(out, msgs[hidden:]...)
	return out
}
