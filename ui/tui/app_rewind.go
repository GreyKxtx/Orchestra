package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/view"
)

type sessionRewindResultMsg struct {
	label string
	err   error
}

func (a *App) openRewindDialog() {
	if a.turn.ShowBusySpinner() {
		a.showToast("дождитесь конца хода")
		return
	}
	a.showWelcome = false
	a.chat.SetForceWelcome(false)
	items := a.rewindCheckpoints()
	if len(items) == 0 {
		a.showToast("нет user-сообщений для rewind")
		return
	}
	a.pushDialog(view.NewRewindDialog(items))
}

func (a *App) rewindCheckpoints() []view.RewindCheckpoint {
	var out []view.RewindCheckpoint
	for i, m := range a.session.Messages {
		if m.Role != state.RoleUser {
			continue
		}
		label := strings.TrimSpace(m.Text)
		if label == "" {
			label = "(пустое сообщение)"
		}
		cp := view.RewindCheckpoint{
			MsgIndex: i,
			Label:    label,
		}
		if !m.StartedAt.IsZero() {
			cp.At = m.StartedAt
		}
		if len([]rune(cp.Label)) > 60 {
			cp.Label = string([]rune(cp.Label)[:59]) + "…"
		}
		out = append(out, cp)
	}
	return out
}

func (a *App) handleRewindSelect(cp view.RewindCheckpoint) tea.Cmd {
	if a.turn.ShowBusySpinner() {
		a.showToast("дождитесь конца хода")
		return nil
	}
	return a.rewindToCheckpointCmd(cp)
}

func (a *App) rewindToCheckpointCmd(cp view.RewindCheckpoint) tea.Cmd {
	idx := cp.MsgIndex
	msgs := a.session.Messages
	if idx < 0 || idx >= len(msgs) || msgs[idx].Role != state.RoleUser {
		a.showToast("некорректный checkpoint")
		return nil
	}

	label := strings.TrimSpace(cp.Label)
	a.session.SetMessages(append([]state.Message(nil), msgs[:idx+1]...))
	a.resetStateAfterRewind()
	a.chat.SyncExpandFromMessages(a.session.Messages)
	a.chat.SetMessages(a.session.Messages)
	a.layout()
	a.updateStatusHints()

	if a.rpc == nil || a.coreSessionID == "" {
		a.showToast("rewind · " + label + " (offline)")
		return a.persistSessionCmd()
	}

	sid := a.coreSessionID
	rpc := a.rpc
	return func() tea.Msg {
		_, err := rpc.SessionRewind(context.Background(), sid, idx)
		return sessionRewindResultMsg{label: label, err: err}
	}
}

func (a *App) resetStateAfterRewind() {
	a.lastCommitDiff = nil
	a.diffShown = false
	a.pendingOps = nil
	a.pendingReview = false
	a.diffCursor = 0
	a.msgQueue = nil
	a.setTodos(nil)
	a.syncDiffReviewCursor()
	a.restorePromptTokensFromSession()
}

func (a *App) handleSessionRewindResult(m sessionRewindResultMsg) tea.Cmd {
	if m.err != nil {
		a.session.AppendSystemNotice(state.SystemKindError, "rewind: "+m.err.Error())
		a.chat.SetMessages(a.session.Messages)
		return a.persistSessionCmd()
	}
	a.showToast("rewind · " + m.label)
	return a.persistSessionCmd()
}
