package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/internal/sessionfile"
	"github.com/orchestra/orchestra/internal/sessionstore"
	"github.com/orchestra/orchestra/ui/tui/rpcclient"
	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/view"
)

type coreSessionStartedMsg struct {
	sessionID string
	restored  bool
	got       *rpcclient.SessionGetResult // prefetched session.get (nil on error)
	err       error
}

// startCoreSessionTimeout bounds session.start + session.get: without it a
// hung core would leak the goroutine forever (the Cmd never returns).
const startCoreSessionTimeout = 30 * time.Second

// startCoreSession opens or creates the unified v2 session in core.
// When currentSessionID is set (reopen), the same id is passed so agent
// history and UI projection reload from one on-disk snapshot.
// SessionGet runs here too — inside the Cmd goroutine — so Update() never
// performs a blocking RPC round-trip (a slow core would freeze the UI).
func (a *App) startCoreSession() tea.Cmd {
	if a.rpc == nil {
		return nil
	}
	rpc := a.rpc
	sessionID := strings.TrimSpace(a.currentSessionID)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), startCoreSessionTimeout)
		defer cancel()
		sid, restored, err := rpc.SessionStart(ctx, sessionID)
		if err != nil {
			return coreSessionStartedMsg{sessionID: sid, restored: restored, err: err}
		}
		got, gErr := rpc.SessionGet(ctx, sid)
		if gErr != nil {
			got = nil
		}
		return coreSessionStartedMsg{sessionID: sid, restored: restored, got: got}
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
	got := m.got
	if got == nil {
		return
	}
	if len(a.session.Messages) == 0 && len(got.UIMessages) > 0 {
		a.session = state.NewSession()
		for _, msg := range stateMessagesFromUI(got.UIMessages) {
			a.session.AppendMessage(msg)
		}
		a.chat.SetMessages(a.session.Messages)
		a.showWelcome = false
		a.chat.SetForceWelcome(false)
	}
	a.setTodos(got.Todos)
	if got.CostUSD > 0 {
		a.chrome.sessionCostUSD = got.CostUSD
	}
	a.chat.SyncExpandFromMessages(a.session.Messages)
	a.syncDiffStateFromSession()
	a.restorePromptTokensFromSession()
	a.updateStatusHints()

	// Cross-process turn state (resilience follow-up): a detached background
	// core may still be finishing a paid turn, or the previous holder died.
	if got.ExternalTurn {
		a.session.AppendSystemNotice(state.SystemKindInfo,
			"Предыдущий ход этой сессии ещё завершается в фоновом процессе — новый ход будет отклонён, пока он не закончит. Переоткройте сессию позже, чтобы увидеть результат.")
		a.chat.SetMessages(a.session.Messages)
	} else if got.Interrupted {
		a.session.AppendSystemNotice(state.SystemKindInfo,
			"Предыдущий ход был прерван (процесс завершился аварийно). История сохранена до последнего выполненного шага.")
		a.chat.SetMessages(a.session.Messages)
	}

	// Surface whether agent LLM history actually came back. UI scrollback can
	// look full while history_len=0 (failed turns used to skip ReplaceHistory).
	if m.restored || got.HistoryLen > 0 || len(got.UIMessages) > 0 || len(a.session.Messages) > 0 {
		switch {
		case got.HistoryLen > 0:
			a.showToast(fmt.Sprintf("session · history %d msgs", got.HistoryLen))
		case len(a.session.Messages) > 0:
			a.showToast("session · UI only (agent history empty)")
			a.session.AppendSystemNotice(state.SystemKindInfo,
				"LLM-история сессии пуста — модель не помнит прошлые tool results. Продолжение начнёт исследование заново.")
			a.chat.SetMessages(a.session.Messages)
		}
	}
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
	cost := a.chrome.sessionCostUSD
	msgs := append([]state.Message(nil), a.session.Messages...)
	ui := uiMessagesFromState(msgs)
	title := sessionstore.TitleFromMessages(msgs)
	rpc := a.rpc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := rpc.SessionUISync(ctx, id, title, model, ui, cost); err != nil {
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
	a.setTodos(nil)

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
			a.chrome.promptTokensUsed = m.PromptCtx
		case m.TokensIn > 0 && (limit <= 0 || m.TokensIn <= limit):
			a.chrome.promptTokensUsed = m.TokensIn
		default:
			continue
		}
		a.chrome.livePromptTokens = 0
		a.syncStatusBar()
		return
	}
}

// openSessionsDialog lists project sessions (.orchestra/sessions) for pick/delete.
func (a *App) openSessionsDialog() {
	metas, err := sessionstore.List(a.cfg.WorkspaceRoot)
	if err != nil || len(metas) == 0 {
		a.showToast("no sessions in this project")
		return
	}
	a.dialogStack = append(a.dialogStack, view.NewSessionsDialog(metas))
}

// sessionsMatchingQuery returns the metadata of sessions whose message text
// contains query, preserving ListMeta's most-recently-updated-first order.
func (a *App) sessionsMatchingQuery(query string) ([]sessionstore.SessionMeta, error) {
	hits, err := sessionfile.Search(a.cfg.WorkspaceRoot, sessionfile.SearchOptions{
		Query:       query,
		Insensitive: true, // typing a filter in a picker is not a case-sensitive act
	})
	if err != nil {
		return nil, err
	}
	// Build the metas straight from the hits. Search already parsed every
	// session file to produce them, groups its hits by session, and orders the
	// groups by UpdatedAt descending — the same order sessionstore.List uses.
	// Calling List afterwards parsed all of them a second time to learn what
	// the hits already carry.
	//
	// A Hit carries the id, title and update time the picker renders. The
	// message count and model it would otherwise show stay zero, and the
	// dialog omits both when they are unset.
	out := make([]sessionstore.SessionMeta, 0, len(hits))
	seen := make(map[string]bool, len(hits))
	for _, h := range hits {
		if seen[h.SessionID] {
			continue
		}
		seen[h.SessionID] = true
		out = append(out, sessionstore.SessionMeta{
			ID:        h.SessionID,
			Title:     h.Title,
			UpdatedAt: h.UpdatedAt,
		})
	}
	return out, nil
}

// openSessionsDialogFiltered opens the session picker seeded with only the
// sessions whose text matches query. The dialog itself is unchanged — it
// already accepts a prepared list, and its own fuzzy filter over titles keeps
// working on top of the narrowed set.
func (a *App) openSessionsDialogFiltered(query string) {
	metas, err := a.sessionsMatchingQuery(query)
	if err != nil {
		a.showToast("поиск по сессиям: " + err.Error())
		return
	}
	if len(metas) == 0 {
		a.showToast("ничего не найдено: " + query)
		return
	}
	a.dialogStack = append(a.dialogStack, view.NewSessionsDialog(metas))
}
