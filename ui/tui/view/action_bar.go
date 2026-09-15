package view

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// ActionBarState describes pending dry-run ops shown inline in the chat stream.
type ActionBarState struct {
	OpCount   int
	FileCount int
	Expanded  bool // diff panel expanded
	Review    bool // awaiting user apply (pendingReview)
}

// RenderActionBar draws the OpenCode-style inline bar:
// ⏵ N pending ops · [a]pply · [d]iff · [x]discard
//
// Only for ops awaiting apply. A diff a turn has already written is not
// pending and has no batch to discard; its own header carries the per-file
// keys.
func RenderActionBar(st ActionBarState, width int) string {
	if !st.Review || (st.OpCount <= 0 && st.FileCount <= 0) {
		return ""
	}
	t := ThemeForApp()
	accent := lipgloss.NewStyle().Foreground(t.Primary()).Bold(true)
	muted := lipgloss.NewStyle().Foreground(t.TextMuted())
	key := lipgloss.NewStyle().Foreground(t.Warning()).Bold(true)

	n := st.OpCount
	if n <= 0 {
		n = st.FileCount
	}
	label := fmt.Sprintf("⏵ %d pending", n)
	if st.FileCount > 1 {
		label = fmt.Sprintf("⏵ %d ops · %d files", st.OpCount, st.FileCount)
	}

	hints := key.Render("[a]") + "pply · " + key.Render("[d]") + "iff · " + key.Render("[x]") + "discard"
	if st.Expanded {
		hints = key.Render("[a]") + " accept · " + key.Render("[x]") + " reject · Enter apply · " + key.Render("[d]") + " collapse"
	}

	line := accent.Render(label) + muted.Render(" · ") + muted.Render(hints)
	if width > 4 && lipgloss.Width(line) > width {
		line = lipgloss.NewStyle().Width(width).Render(line)
	}
	return line
}
