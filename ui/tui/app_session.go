package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/internal/sessionstore"
	"github.com/orchestra/orchestra/ui/tui/state"
)

type coreSessionStartedMsg struct {
	sessionID string
	err       error
}

// startCoreSession allocates a new JSON-RPC session in the core process so
// multi-turn chat preserves agent history and todos between user messages.
func (a *App) startCoreSession() tea.Cmd {
	if a.rpc == nil {
		return nil
	}
	rpc := a.rpc
	return func() tea.Msg {
		sid, err := rpc.SessionStart(context.Background())
		return coreSessionStartedMsg{sessionID: sid, err: err}
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

// persistSessionCmd saves the TUI chat transcript to .orchestra/sessions/.
func (a *App) persistSessionCmd() tea.Cmd {
	if a.cfg.WorkspaceRoot == "" || len(a.session.Messages) == 0 {
		return nil
	}
	if a.currentSessionID == "" {
		a.currentSessionID = sessionstore.NewID()
	}
	root := a.cfg.WorkspaceRoot
	id := a.currentSessionID
	model := a.cfg.Model
	msgs := append([]state.Message(nil), a.session.Messages...)
	return func() tea.Msg {
		rec := &sessionstore.SessionRecord{
			SessionMeta: sessionstore.SessionMeta{
				ID:        id,
				Title:     sessionstore.TitleFromMessages(msgs),
				Model:     model,
				CreatedAt: msgs[0].StartedAt,
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

// loadSession restores a saved TUI chat from disk (display only; starts a fresh core session).
func (a *App) loadSession(id string) {
	rec, err := sessionstore.Load(a.cfg.WorkspaceRoot, id)
	if err != nil {
		a.session.AppendMessage(state.Message{
			Role: state.RoleSystem,
			Text: "[error] load session: " + err.Error(),
		})
		a.chat.SetMessages(a.session.Messages)
		return
	}
	a.session = state.NewSession()
	for _, m := range rec.Messages {
		a.session.AppendMessage(m)
	}
	a.currentSessionID = rec.ID
	a.coreSessionID = ""
	a.chat.SetMessages(a.session.Messages)
	a.showWelcome = false
	a.chat.SetForceWelcome(false)
}
