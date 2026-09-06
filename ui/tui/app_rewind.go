package tui

import (
	"context"
	"strings"
	"time"

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
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := rpc.SessionRewind(ctx, sid, idx)
		return sessionRewindResultMsg{label: label, err: err}
	}
}

func (a *App) resetStateAfterRewind() {
	a.lastCommitDiff = nil
	a.review.Reset()
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

type sessionForkResultMsg struct {
	label string
	id    string
	err   error
}

func (a *App) openForkDialog() {
	if a.turn.ShowBusySpinner() {
		a.showToast("дождитесь конца хода")
		return
	}
	if a.rpc == nil || a.coreSessionID == "" {
		a.showToast("fork требует активную сессию ядра")
		return
	}
	a.showWelcome = false
	a.chat.SetForceWelcome(false)
	items := a.rewindCheckpoints()
	// The first user message cannot be a fork point: the branch would be empty.
	if len(items) > 0 && items[0].MsgIndex == 0 {
		items = items[1:]
	}
	if len(items) == 0 {
		a.showToast("нет точек для ветки")
		return
	}
	a.pushDialog(view.NewForkDialog(items))
}

// forkAtCheckpointCmd branches the session at cp without touching the current
// one — the difference from rewind, which truncates in place.
func (a *App) forkAtCheckpointCmd(cp view.RewindCheckpoint) tea.Cmd {
	if a.turn.ShowBusySpinner() {
		a.showToast("дождитесь конца хода")
		return nil
	}
	if a.rpc == nil || a.coreSessionID == "" {
		a.showToast("fork требует активную сессию ядра")
		return nil
	}
	idx := cp.MsgIndex
	msgs := a.session.Messages
	if idx <= 0 || idx >= len(msgs) || msgs[idx].Role != state.RoleUser {
		a.showToast("некорректная точка ветки")
		return nil
	}

	label := strings.TrimSpace(cp.Label)
	sid := a.coreSessionID
	rpc := a.rpc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		res, err := rpc.SessionFork(ctx, sid, idx)
		if err != nil {
			return sessionForkResultMsg{label: label, err: err}
		}
		return sessionForkResultMsg{label: label, id: res.SessionID}
	}
}

func (a *App) handleSessionForkResult(m sessionForkResultMsg) tea.Cmd {
	if m.err != nil {
		a.session.AppendSystemNotice(state.SystemKindError, "fork: "+m.err.Error())
		a.chat.SetMessages(a.session.Messages)
		return nil
	}
	a.showToast("ветка · " + m.label)
	// Switching in is the point of "try step N differently": the parent stays
	// on disk and is reachable from /sessions.
	return a.loadSession(m.id)
}
