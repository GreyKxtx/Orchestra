package state

import "fmt"

// TurnState is the TUI turn lifecycle (replaces ad-hoc agentBusy flags).
type TurnState string

const (
	TurnIdle      TurnState = "idle"
	TurnComposing TurnState = "composing"
	TurnRunning   TurnState = "running"
	TurnApplying  TurnState = "applying"
)

// ErrTurnBusy is returned when a transition is invalid for the current state.
var ErrTurnBusy = fmt.Errorf("turn busy")

// TurnFSM gates submit, cancel, and apply flows for one user turn.
type TurnFSM struct {
	State TurnState
}

// NewTurnFSM returns a turn machine in the idle state.
func NewTurnFSM() *TurnFSM {
	return &TurnFSM{State: TurnIdle}
}

// BlocksSubmit reports whether Enter must be ignored (agent/workflow/apply in flight).
func (f *TurnFSM) BlocksSubmit() bool {
	return f.State == TurnRunning || f.State == TurnApplying
}

// BlocksInput reports whether the chat input should reject new messages.
func (f *TurnFSM) BlocksInput() bool {
	return f.BlocksSubmit()
}

// CanCancel reports whether Esc should cancel the in-flight RPC turn.
func (f *TurnFSM) CanCancel() bool {
	return f.State == TurnRunning
}

// CanApplyPending reports whether manual ops apply ([a] / /apply) is allowed.
func (f *TurnFSM) CanApplyPending() bool {
	return f.State == TurnIdle || f.State == TurnComposing
}

// ShowBusySpinner drives status bar / chat busy indicators.
func (f *TurnFSM) ShowBusySpinner() bool {
	return f.State == TurnRunning || f.State == TurnApplying
}

// IsRunning is true while an agent/workflow/skill turn is in flight.
func (f *TurnFSM) IsRunning() bool { return f.State == TurnRunning }

// IsApplying is true while ops.apply is in flight.
func (f *TurnFSM) IsApplying() bool { return f.State == TurnApplying }

// OnInputChange updates composing vs idle from textarea content.
func (f *TurnFSM) OnInputChange(nonEmpty bool) {
	switch f.State {
	case TurnIdle:
		if nonEmpty {
			f.State = TurnComposing
		}
	case TurnComposing:
		if !nonEmpty {
			f.State = TurnIdle
		}
	}
}

// OnSubmit transitions to running when idle/composing.
func (f *TurnFSM) OnSubmit() error {
	if f.BlocksSubmit() {
		return ErrTurnBusy
	}
	f.State = TurnRunning
	return nil
}

// OnTurnComplete returns to idle after a successful agent/workflow turn.
func (f *TurnFSM) OnTurnComplete() {
	f.State = TurnIdle
}

// OnTurnError returns to idle after a failed/cancelled turn.
func (f *TurnFSM) OnTurnError() {
	f.State = TurnIdle
}

// OnApplyStart transitions to applying.
func (f *TurnFSM) OnApplyStart() error {
	if !f.CanApplyPending() {
		return ErrTurnBusy
	}
	f.State = TurnApplying
	return nil
}

// OnApplyComplete returns to idle after ops.apply finishes.
func (f *TurnFSM) OnApplyComplete() {
	f.State = TurnIdle
}

// Reset forces idle (session clear, reconnect).
func (f *TurnFSM) Reset() {
	f.State = TurnIdle
}
