package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/internal/sessionstore"
	"github.com/orchestra/orchestra/ui/tui/state"
)

// persistSessionCmd returns a tea.Cmd that snapshots the current session and
// writes it to .orchestra/sessions/<id>.json. No-op if there's no workspace
// root or no messages to save.
func (a *App) persistSessionCmd() tea.Cmd {
	if a.cfg.WorkspaceRoot == "" {
		return nil
	}
	if len(a.session.Messages) == 0 {
		return nil
	}
	if a.currentSessionID == "" {
		a.currentSessionID = sessionstore.NewID()
	}
	// Snapshot to avoid racing with concurrent mutations once we hand to a
	// goroutine. State.Message has no pointer fields except slices we
	// shouldn't mutate further from this id forward.
	msgs := make([]state.Message, len(a.session.Messages))
	copy(msgs, a.session.Messages)
	rec := sessionstore.SessionRecord{
		SessionMeta: sessionstore.SessionMeta{
			ID:    a.currentSessionID,
			Title: sessionstore.TitleFromMessages(msgs),
			Model: a.cfg.Model,
		},
		Messages: msgs,
	}
	root := a.cfg.WorkspaceRoot
	return func() tea.Msg {
		_ = sessionstore.Save(root, &rec)
		return nil
	}
}

// loadSession replaces the visible chat with the on-disk record for id.
// The agent context is NOT restored — sending a new message starts a fresh
// agent run; loaded history is for the user to read.
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
	a.session.SetMessages(rec.Messages)
	a.currentSessionID = rec.ID
	a.showWelcome = false
	a.chat.SetForceWelcome(false)
	a.chat.SetMessages(a.session.Messages)
	a.session.AppendMessage(state.Message{
		Role: state.RoleSystem,
		Text: fmt.Sprintf("[loaded session: %s — agent has no memory of this history]", rec.Title),
	})
	a.chat.SetMessages(a.session.Messages)
}
