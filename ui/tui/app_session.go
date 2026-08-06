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
	if m.restored && a.rpc != nil {
		if got, err := a.rpc.SessionGet(context.Background(), m.sessionID); err == nil {
			if len(a.session.Messages) == 0 && len(got.UIMessages) > 0 {
				a.session = state.NewSession()
				for _, msg := range stateMessagesFromUI(got.UIMessages) {
					a.session.AppendMessage(msg)
				}
				a.chat.SetMessages(a.session.Messages)
				a.showWelcome = false
				a.chat.SetForceWelcome(false)
			}
			if len(got.Todos) > 0 {
				a.setTodos(got.Todos)
			}
			if got.CostUSD > 0 {
				a.sessionCostUSD = got.CostUSD
			}
			a.chat.SyncExpandFromMessages(a.session.Messages)
			a.syncDiffStateFromSession()
			a.restorePromptTokensFromSession()
			a.updateStatusHints()
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
	cost := a.sessionCostUSD
	msgs := append([]state.Message(nil), a.session.Messages...)
	ui := uiMessagesFromState(msgs)
	title := sessionstore.TitleFromMessages(msgs)
	rpc := a.rpc
	return func() tea.Msg {
		if err := rpc.SessionUISync(context.Background(), id, title, model, ui, cost); err != nil {
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
	a.chat.SyncExpandFromMessages(a.session.Messages)
	a.chat.SetMessages(a.session.Messages)
	a.showWelcome = false
	a.chat.SetForceWelcome(false)
	a.syncDiffStateFromSession()
	a.restorePromptTokensFromSession()
	a.updateStatusHints()
	if a.rpc != nil {
		return a.startCoreSession()
	}
	return nil
}

// restorePromptTokensFromSession sets the status-bar token counter from the
// last assistant turn. Prefer PromptCtx (last per-step prompt size). TokensIn
// is a turn total (sum of steps) and is only used as a fallback when it cannot
// exceed the configured context limit (typical single-step turns).
func (a *App) restorePromptTokensFromSession() {
	limit := a.contextLimit()
	for i := len(a.session.Messages) - 1; i >= 0; i-- {
		m := a.session.Messages[i]
		if m.Role != state.RoleAssistant {
			continue
		}
		switch {
		case m.PromptCtx > 0:
			a.promptTokensUsed = m.PromptCtx
		case m.TokensIn > 0 && (limit <= 0 || m.TokensIn <= limit):
			a.promptTokensUsed = m.TokensIn
		default:
			continue
		}
		a.livePromptTokens = 0
		a.syncStatusBar()
		return
	}
}
