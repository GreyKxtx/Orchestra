package tui

import (
	"strings"

	"github.com/orchestra/orchestra/ui/tui/state"
)

// syncTurnUI pushes TurnFSM busy state into status bar and chat widgets.
func (a *App) syncTurnUI() {
	busy := a.turn.ShowBusySpinner()
	a.statusBar.SetAgentBusy(busy)
	a.chat.SetAgentBusy(busy)
}

// syncTurnComposing updates idle/composing from the textarea contents.
func (a *App) syncTurnComposing() {
	a.turn.OnInputChange(strings.TrimSpace(a.input.Value()) != "")
}

// beginApplyTurn transitions to applying and refreshes UI chrome.
func (a *App) beginApplyTurn() bool {
	if err := a.turn.OnApplyStart(); err != nil {
		return false
	}
	a.syncTurnUI()
	a.layout()
	a.updateStatusHints()
	return true
}

// finishApplyTurn returns to idle after ops.apply completes.
func (a *App) finishApplyTurn() {
	a.turn.OnApplyComplete()
	a.syncTurnUI()
	a.layout()
	a.updateStatusHints()
}

// beginAgentTurn marks the turn as running and wires cancel UI.
func (a *App) beginAgentTurn() {
	_ = a.turn.OnSubmit() // caller already gated BlocksSubmit
	a.syncTurnUI()
}

// finishAgentTurn clears running state after RPC completion or error.
func (a *App) finishAgentTurn() {
	a.turn.OnTurnComplete()
	a.syncTurnUI()
	a.layout()
	a.updateStatusHints()
}

// failAgentTurn clears running state after RPC error/cancel.
func (a *App) failAgentTurn() {
	a.turn.OnTurnError()
	a.syncTurnUI()
	a.layout()
	a.updateStatusHints()
}

// resetTurnFSM forces idle (clear session, reconnect).
func (a *App) resetTurnFSM() {
	a.turn.Reset()
	a.syncTurnUI()
}

// turnState exposes the current FSM state for views/tests.
func (a *App) turnState() state.TurnState {
	return a.turn.State
}
