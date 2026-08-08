package tui

import (
	"github.com/orchestra/orchestra/ui/tui/rpcclient"
	"github.com/orchestra/orchestra/ui/tui/state"
)

// buildDiffFiles converts lastCommitDiff to session DiffFile slice.
func (a *App) buildDiffFiles() []state.DiffFile {
	if len(a.lastCommitDiff) == 0 {
		return nil
	}
	out := make([]state.DiffFile, len(a.lastCommitDiff))
	for i, fd := range a.lastCommitDiff {
		out[i] = state.DiffFile{Path: fd.Path, Before: fd.Before, After: fd.After}
	}
	return out
}

func (a *App) showCommitDiff() {
	if len(a.lastCommitDiff) == 0 && !a.session.HasDiff() {
		return
	}
	// Prefer toggling in-chat RoleDiff when lastCommitDiff is empty (reopen).
	if len(a.lastCommitDiff) == 0 {
		if a.session.ToggleLastDiff() {
			a.diffShown = a.session.HasDiff()
			a.syncDiffReviewCursor()
			a.chat.SetMessages(a.session.Messages)
			a.layout()
			a.updateStatusHints()
		}
		return
	}
	if a.diffShown {
		a.session.RemoveDiff()
		a.diffShown = false
		a.chat.SetDiffReviewCursor(-1)
	} else {
		a.session.AddDiffFiles(a.buildDiffFiles())
		a.session.ExpandLastDiff()
		a.diffShown = true
		a.diffCursor = 0
	}
	a.syncDiffReviewCursor()
	a.chat.SetMessages(a.session.Messages)
	a.layout()
	a.updateStatusHints()
}

// syncDiffStateFromSession rebuilds lastCommitDiff from RoleDiff messages so
// /diff and d work after session reopen.
func (a *App) syncDiffStateFromSession() {
	a.lastCommitDiff = nil
	a.diffShown = false
	for _, m := range a.session.Messages {
		if m.Role != state.RoleDiff || len(m.DiffFiles) == 0 {
			continue
		}
		out := make([]rpcclient.FileDiff, len(m.DiffFiles))
		for i, df := range m.DiffFiles {
			out[i] = rpcclient.FileDiff{Path: df.Path, Before: df.Before, After: df.After}
		}
		a.lastCommitDiff = out
		a.diffShown = true
		if m.DiffExpanded {
			a.diffCursor = 0
		}
		return
	}
}
