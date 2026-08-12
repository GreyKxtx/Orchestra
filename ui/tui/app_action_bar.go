package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/view"
)

// actionBarActive reports whether batch-level [a]/[d]/[x] apply to pending dry-run ops.
func (a *App) actionBarActive() bool {
	if a.turn.ShowBusySpinner() || a.permModal != nil || a.questionModal != nil {
		return false
	}
	if !a.inputEmpty() {
		return false
	}
	return a.review.HasPendingOps()
}

func (a *App) actionBarState() view.ActionBarState {
	st := view.ActionBarState{
		OpCount:   len(a.review.PendingOps()),
		FileCount: a.session.DiffFileCount(),
		Expanded:  a.session.LastDiffExpanded(),
		Review:    a.review.PendingReview(),
	}
	if st.FileCount == 0 && len(a.lastCommitDiff) > 0 {
		st.FileCount = len(a.lastCommitDiff)
	}
	return st
}

func (a *App) syncActionBar() {
	a.chat.SetActionBar(a.actionBarState())
}

// tryActionBarHotkey handles batch [a]/[d]/[x] when diff review is collapsed.
// When expanded, diff review owns a/x; d collapses; shift+x discards batch.
func (a *App) tryActionBarHotkey(key string) (tea.Cmd, bool) {
	if !a.actionBarActive() {
		return nil, false
	}
	expanded := a.session.LastDiffExpanded()

	switch key {
	case "d":
		a.showCommitDiff()
		return nil, true
	case "shift+x":
		return a.discardPendingOpsCmd(), true
	case "x":
		if expanded && a.diffReviewActive() {
			return nil, false // per-file reject in tryDiffReviewHotkey
		}
		return a.discardPendingOpsCmd(), true
	case "a":
		if expanded && a.diffReviewActive() {
			return nil, false // per-file accept in tryDiffReviewHotkey
		}
		a.session.AcceptAllDiffFiles()
		a.refreshDiffView()
		return a.applyPendingDiffReviewCmd(), true
	default:
		return nil, false
	}
}

func (a *App) discardPendingOps() {
	a.review.Reset()
	a.lastCommitDiff = nil
	a.session.RemoveDiff()
	a.syncDiffReviewCursor()
	a.syncActionBar()
	a.chat.SetMessages(a.session.Messages)
	a.session.AppendSystemNotice(state.SystemKindInfo, "pending changes discarded")
	a.layout()
	a.updateStatusHints()
}

func (a *App) discardPendingOpsCmd() tea.Cmd {
	a.discardPendingOps()
	return a.persistSessionCmd()
}
