package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/ui/tui/rpcclient"
	"github.com/orchestra/orchestra/ui/tui/view"
)

// showRuleSuggestion opens the lesson-rule modal for a repeated anti-pattern,
// unless another permission modal is already showing — a rule suggestion is
// low-urgency compared to a pending exec/lsp consent, and it will fire again
// on the next occurrence if skipped this turn.
func (a *App) showRuleSuggestion(sug *rpcclient.RuleSuggestion) {
	if sug == nil || a.permModal != nil {
		return
	}
	a.ruleSuggestion = sug
	a.permModal = view.NewPermissionModal("", sug.Text, "lesson_rule")
	a.layout()
}

// respondRuleSuggestion answers the current lesson-rule modal and clears it.
func (a *App) respondRuleSuggestion(accept bool) tea.Cmd {
	sug := a.ruleSuggestion
	a.ruleSuggestion = nil
	a.permModal = nil
	a.layout()
	if accept {
		a.showToast("добавлено в ORCHESTRA.md")
	} else {
		a.showToast("пропущено")
	}
	if a.rpc == nil || sug == nil {
		return nil
	}
	rpc := a.rpc
	s := *sug
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = rpc.RespondRuleSuggestion(ctx, accept, s)
		return nil
	}
}
