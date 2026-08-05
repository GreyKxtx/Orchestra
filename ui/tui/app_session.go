package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/internal/sessionfile"
	"github.com/orchestra/orchestra/internal/sessionstore"
	"github.com/orchestra/orchestra/ui/tui/state"
)

type coreSessionStartedMsg struct {
	sessionID string
	restored  bool
	err       error
}

// startCoreSession opens or creates the unified v2 session in core.
// When currentSessionID is set (reopen), the same id is passed so agent
// history and UI projection reload from one on-disk snapshot.
func (a *App) startCoreSession() tea.Cmd {
	if a.rpc == nil {
		return nil
	}
	rpc := a.rpc
	sessionID := strings.TrimSpace(a.currentSessionID)
	return func() tea.Msg {
		sid, restored, err := rpc.SessionStart(context.Background(), sessionID)
		return coreSessionStartedMsg{sessionID: sid, restored: restored, err: err}
	}
}

func (a *App) handleCoreSessionStarted(m coreSessionStartedMsg) {
	if m.err != nil {
		a.session.AppendMessage(state.Message{
			Role: state.RoleSystem,
			Text: "[error] session.start: " + m.err.Error(),
		})
		a.chat.SetMessages(a.session.Messages)
		return
	}
	a.coreSessionID = m.sessionID
	a.currentSessionID = m.sessionID
	if m.restored && a.rpc != nil && len(a.session.Messages) == 0 {
		if got, err := a.rpc.SessionGet(context.Background(), m.sessionID); err == nil && len(got.UIMessages) > 0 {
			a.session = state.NewSession()
			for _, msg := range stateMessagesFromUI(got.UIMessages) {
				a.session.AppendMessage(msg)
			}
			a.chat.SetMessages(a.session.Messages)
			a.showWelcome = false
			a.chat.SetForceWelcome(false)
		}
	}
}

// runAgentTurn starts an agent turn via session.message when a core session
// exists, otherwise falls back to one-shot agent.run.
func (a *App) runAgentTurn(ctx context.Context, query, mode string) error {
	opts := a.agentRunOptions()
	if a.coreSessionID != "" {
		return a.rpc.SessionMessage(ctx, a.coreSessionID, query, mode, opts)
	}
	return a.rpc.AgentRun(ctx, query, mode, opts)
}

// persistSessionCmd saves UI projection via core session.ui_sync (unified v2).
func (a *App) persistSessionCmd() tea.Cmd {
	if a.cfg.WorkspaceRoot == "" || len(a.session.Messages) == 0 {
		return nil
	}
	if a.currentSessionID == "" {
		a.currentSessionID = sessionfile.NewID()
	}
	if a.coreSessionID == "" {
		a.coreSessionID = a.currentSessionID
	}
	if a.rpc == nil {
		return a.persistSessionOfflineCmd()
	}
	id := a.currentSessionID
	model := a.cfg.Model
	msgs := append([]state.Message(nil), a.session.Messages...)
	ui := uiMessagesFromState(msgs)
	title := sessionstore.TitleFromMessages(msgs)
	rpc := a.rpc
	return func() tea.Msg {
		if err := rpc.SessionUISync(context.Background(), id, title, model, ui); err != nil {
			return sessionPersistMsg{err: err}
		}
		return sessionPersistMsg{id: id}
	}
}

// persistSessionOfflineCmd writes UI via sessionfile when core is unavailable.
func (a *App) persistSessionOfflineCmd() tea.Cmd {
	root := a.cfg.WorkspaceRoot
	id := a.currentSessionID
	model := a.cfg.Model
	msgs := append([]state.Message(nil), a.session.Messages...)
	return func() tea.Msg {
		rec := &sessionstore.SessionRecord{
			SessionMeta: sessionstore.SessionMeta{
				ID:    id,
				Title: sessionstore.TitleFromMessages(msgs),
				Model: model,
			},
			Messages: msgs,
		}
		if err := sessionstore.Save(root, rec); err != nil {
			return sessionPersistMsg{err: err}
		}
		return sessionPersistMsg{id: id}
	}
}

type sessionPersistMsg struct {
	id  string
	err error
}

// loadSession restores chat from disk and rebinds core agent history.
func (a *App) loadSession(id string) tea.Cmd {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	a.currentSessionID = id
	a.coreSessionID = ""
	a.session = state.NewSession()

	rec, err := sessionstore.Load(a.cfg.WorkspaceRoot, id)
	if err != nil {
		a.session.AppendMessage(state.Message{
			Role: state.RoleSystem,
			Text: "[error] load session: " + err.Error(),
		})
	} else {
		for _, m := range rec.Messages {
			a.session.AppendMessage(m)
		}
	}
	a.chat.SetMessages(a.session.Messages)
	a.showWelcome = false
	a.chat.SetForceWelcome(false)
	if a.rpc != nil {
		return a.startCoreSession()
	}
	return nil
}
