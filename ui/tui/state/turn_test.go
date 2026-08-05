package state

import "testing"

func TestTurnFSM_SubmitAndComplete(t *testing.T) {
	f := NewTurnFSM()
	if err := f.OnSubmit(); err != nil {
		t.Fatal(err)
	}
	if f.State != TurnRunning {
		t.Fatalf("state=%q", f.State)
	}
	if !f.BlocksSubmit() {
		t.Fatal("expected blocked submit while running")
	}
	f.OnTurnComplete()
	if f.State != TurnIdle {
		t.Fatalf("after complete: %q", f.State)
	}
}

func TestTurnFSM_Composing(t *testing.T) {
	f := NewTurnFSM()
	f.OnInputChange(true)
	if f.State != TurnComposing {
		t.Fatalf("state=%q", f.State)
	}
	f.OnInputChange(false)
	if f.State != TurnIdle {
		t.Fatalf("state=%q", f.State)
	}
}

func TestTurnFSM_ApplyGated(t *testing.T) {
	f := NewTurnFSM()
	if err := f.OnApplyStart(); err != nil {
		t.Fatal(err)
	}
	if f.CanApplyPending() {
		t.Fatal("apply should be blocked while applying")
	}
	f.OnApplyComplete()
	if err := f.OnApplyStart(); err != nil {
		t.Fatal(err)
	}
}

func TestTurnFSM_SubmitWhileRunning(t *testing.T) {
	f := NewTurnFSM()
	_ = f.OnSubmit()
	if err := f.OnSubmit(); err != ErrTurnBusy {
		t.Fatalf("want ErrTurnBusy, got %v", err)
	}
}

func TestTurnFSM_ApplyWhileRunning(t *testing.T) {
	f := NewTurnFSM()
	_ = f.OnSubmit()
	if err := f.OnApplyStart(); err != ErrTurnBusy {
		t.Fatalf("want ErrTurnBusy, got %v", err)
	}
}
