package agent

import (
	"errors"
	"strings"
	"testing"
)

// TestSafeRun_ConvertsPanicToRecoveredValue ensures a panicking callback
// does not propagate.
func TestSafeRun_ConvertsPanicToRecoveredValue(t *testing.T) {
	rec := safeRun("test", func() {
		panic("boom")
	})
	if rec == nil {
		t.Fatal("expected non-nil recovered value")
	}
	if s, _ := rec.(string); s != "boom" {
		t.Errorf("recovered=%v, want \"boom\"", rec)
	}
}

// TestSafeRunErr_ConvertsPanicToError ensures a panic in fn becomes a
// regular error with a stack snippet, so the caller can treat panic and
// error uniformly.
func TestSafeRunErr_ConvertsPanicToError(t *testing.T) {
	err := safeRunErr("hook", func() error {
		panic("hook crash")
	})
	if err == nil {
		t.Fatal("expected error from panic, got nil")
	}
	if !strings.Contains(err.Error(), "hook panicked") {
		t.Errorf("error message missing label: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "hook crash") {
		t.Errorf("error message missing recovered value: %q", err.Error())
	}
}

// TestSafeRunErr_PassesThroughOrdinaryErrors ensures non-panic errors flow
// untouched.
func TestSafeRunErr_PassesThroughOrdinaryErrors(t *testing.T) {
	want := errors.New("plain error")
	got := safeRunErr("hook", func() error { return want })
	if !errors.Is(got, want) {
		t.Errorf("want %v, got %v", want, got)
	}
}

// TestSafeRunErr_NilOnSuccess: happy path returns nil.
func TestSafeRunErr_NilOnSuccess(t *testing.T) {
	if err := safeRunErr("hook", func() error { return nil }); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}
