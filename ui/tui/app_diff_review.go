package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/ui/tui/state"
)

type diffRevertResultMsg struct {
	fileIdx int
	path    string
	err     error
}

type diffApplyResultMsg struct {
	applied int
	err     error
}

// diffReviewActive is true when the user can navigate and accept/reject files.
func (a *App) diffReviewActive() bool {
	if a.permModal != nil || a.questionModal != nil || a.turn.ShowBusySpinner() {
		return false
	}
	return a.session.LastDiffExpanded() && a.session.DiffFileCount() > 0
}

func (a *App) syncDiffReviewCursor() {
	if a.diffReviewActive() {
		n := a.session.DiffFileCount()
		if a.diffCursor < 0 {
			a.diffCursor = 0
		}
		if a.diffCursor >= n {
			a.diffCursor = n - 1
		}
		a.chat.SetDiffReviewCursor(a.diffCursor)
		return
	}
	a.diffCursor = 0
	a.chat.SetDiffReviewCursor(-1)
}

func (a *App) refreshDiffView() {
	a.syncDiffReviewCursor()
	a.chat.SetMessages(a.session.Messages)
	a.layout()
	a.updateStatusHints()
}

func (a *App) diffFilePath(fileIdx int) string {
	i := a.session.LastDiffIndex()
	if i < 0 || fileIdx < 0 || fileIdx >= len(a.session.Messages[i].DiffFiles) {
		return ""
	}
	return a.session.Messages[i].DiffFiles[fileIdx].Path
}

func shortPath(path string, max int) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "?"
	}
	r := []rune(path)
	if len(r) <= max {
		return path
	}
	return string(r[:max-1]) + "…"
}

// tryDiffReviewHotkey handles ↑↓ / a / x / Enter while diff is expanded.
func (a *App) tryDiffReviewHotkey(key string) (tea.Cmd, bool) {
	if !a.diffReviewActive() || !a.inputEmpty() {
		return nil, false
	}
	n := a.session.DiffFileCount()
	if n == 0 {
		return nil, false
	}
	switch key {
	case "up", "shift+up":
		if a.diffCursor > 0 {
			a.diffCursor--
			a.refreshDiffView()
		}
		return nil, true
	case "down", "shift+down":
		if a.diffCursor < n-1 {
			a.diffCursor++
			a.refreshDiffView()
		}
		return nil, true
	case "a":
		return a.acceptDiffFileCmd(a.diffCursor), true
	case "x":
		return a.rejectDiffFileCmd(a.diffCursor), true
	case "enter":
		if a.pendingReview {
			return a.applyPendingDiffReviewCmd(), true
		}
		a.session.AcceptAllDiffFiles()
		a.refreshDiffView()
		a.showToast("все файлы приняты")
		return a.persistSessionCmd(), true
	default:
		return nil, false
	}
}

func (a *App) acceptDiffFileCmd(fileIdx int) tea.Cmd {
	if !a.session.SetDiffFileReviewStatus(fileIdx, state.DiffReviewAccepted) {
		return nil
	}
	a.refreshDiffView()
	if a.pendingReview {
		a.showToast("файл принят · Enter — применить")
		return nil
	}
	a.showToast("принято · " + shortPath(a.diffFilePath(fileIdx), 40))
	return a.persistSessionCmd()
}

func (a *App) rejectDiffFileCmd(fileIdx int) tea.Cmd {
	if a.pendingReview {
		a.session.SetDiffFileReviewStatus(fileIdx, state.DiffReviewRejected)
		a.refreshDiffView()
		a.showToast("файл исключён · Enter — применить остальное")
		return a.persistSessionCmd()
	}
	return a.revertDiffFileCmd(fileIdx)
}

func (a *App) revertDiffFileCmd(fileIdx int) tea.Cmd {
	i := a.session.LastDiffIndex()
	if i < 0 || fileIdx < 0 || fileIdx >= len(a.session.Messages[i].DiffFiles) {
		return nil
	}
	df := a.session.Messages[i].DiffFiles[fileIdx]
	if df.ReviewStatus == state.DiffReviewRejected {
		return nil
	}
	a.session.SetDiffFileReviewStatus(fileIdx, state.DiffReviewRejected)
	a.refreshDiffView()
	path := df.Path
	rpc := a.rpc
	return func() tea.Msg {
		if rpc == nil {
			return diffRevertResultMsg{fileIdx: fileIdx, path: path, err: fmt.Errorf("core недоступен")}
		}
		ctx := context.Background()
		var err error
		if df.Before == "" && df.After != "" {
			in, _ := json.Marshal(map[string]string{"path": path})
			err = rpc.ToolCall(ctx, "fs.delete", in)
		} else {
			err = rpc.ApplyOps(ctx, []map[string]any{{
				"op":      "file.write_atomic",
				"path":    path,
				"content": df.Before,
			}})
		}
		return diffRevertResultMsg{fileIdx: fileIdx, path: path, err: err}
	}
}

func (a *App) handleDiffRevertResult(m diffRevertResultMsg) tea.Cmd {
	if m.err != nil {
		a.session.SetDiffFileReviewStatus(m.fileIdx, "")
		a.refreshDiffView()
		a.session.AppendSystemNotice(state.SystemKindError, "откат "+m.path+": "+m.err.Error())
		a.chat.SetMessages(a.session.Messages)
		return a.persistSessionCmd()
	}
	a.syncRevertedFile(m.fileIdx)
	a.session.AppendSystemNotice(state.SystemKindSuccess, "откат: "+m.path)
	a.chat.SetMessages(a.session.Messages)
	a.showToast("отклонено · " + shortPath(m.path, 40))
	return a.persistSessionCmd()
}

func (a *App) syncRevertedFile(fileIdx int) {
	i := a.session.LastDiffIndex()
	if i < 0 || fileIdx < 0 || fileIdx >= len(a.session.Messages[i].DiffFiles) {
		return
	}
	df := a.session.Messages[i].DiffFiles[fileIdx]
	df.After = df.Before
	a.session.Messages[i].DiffFiles[fileIdx] = df
	for j := range a.lastCommitDiff {
		if a.lastCommitDiff[j].Path == df.Path {
			a.lastCommitDiff[j].After = df.Before
		}
	}
}

func (a *App) applyPendingDiffReviewCmd() tea.Cmd {
	if !a.pendingReview || len(a.pendingOps) == 0 || a.rpc == nil {
		return nil
	}
	accepted := a.acceptedDiffPaths()
	if len(accepted) == 0 {
		a.showToast("нет принятых файлов")
		return nil
	}
	ops := filterOpsByPaths(a.pendingOps, accepted)
	if len(ops) == 0 {
		a.showToast("нет ops для принятых файлов")
		return nil
	}
	if !a.beginApplyTurn() {
		return nil
	}
	rpc := a.rpc
	return func() tea.Msg {
		err := rpc.ApplyOps(context.Background(), ops)
		return diffApplyResultMsg{applied: len(ops), err: err}
	}
}

func (a *App) handleDiffApplyResult(m diffApplyResultMsg) tea.Cmd {
	a.finishApplyTurn()
	if m.err != nil {
		a.session.AppendSystemNotice(state.SystemKindError, "apply: "+m.err.Error())
		a.chat.SetMessages(a.session.Messages)
		return a.persistSessionCmd()
	}
	a.pendingOps = nil
	a.pendingReview = false
	a.syncActionBar()
	a.session.AppendSystemNotice(state.SystemKindSuccess,
		fmt.Sprintf("записано на диск: %d ops", m.applied))
	a.chat.SetMessages(a.session.Messages)
	a.refreshDiffView()
	return a.persistSessionCmd()
}

func acceptedDiffPathsFromSession(msgs []state.Message) map[string]bool {
	i := -1
	for j := len(msgs) - 1; j >= 0; j-- {
		if msgs[j].Role == state.RoleDiff && len(msgs[j].DiffFiles) > 0 {
			i = j
			break
		}
	}
	if i < 0 {
		return nil
	}
	out := map[string]bool{}
	for _, df := range msgs[i].DiffFiles {
		if df.ReviewStatus == state.DiffReviewRejected {
			continue
		}
		if p := strings.TrimSpace(df.Path); p != "" {
			out[p] = true
		}
	}
	return out
}

func (a *App) acceptedDiffPaths() map[string]bool {
	return acceptedDiffPathsFromSession(a.session.Messages)
}

func filterOpsByPaths(all []map[string]any, paths map[string]bool) []map[string]any {
	if len(all) == 0 || len(paths) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(all))
	for _, op := range all {
		p, _ := op["path"].(string)
		if paths[p] {
			out = append(out, op)
		}
	}
	return out
}
