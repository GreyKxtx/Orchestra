package agent

import (
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
)

// captureStderr swaps os.Stderr for a pipe, runs fn, and returns whatever
// was written. Used to assert safeRun's panic logging.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w
	var (
		buf  []byte
		wg   sync.WaitGroup
		ioMu sync.Mutex
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		b, _ := io.ReadAll(r)
		ioMu.Lock()
		buf = b
		ioMu.Unlock()
	}()
	fn()
	_ = w.Close()
	wg.Wait()
	os.Stderr = orig
	ioMu.Lock()
	defer ioMu.Unlock()
	return string(buf)
}

// TestSafeRun_ConvertsPanicToRecoveredValue ensures a panicking callback
// does not propagate. captureStderr swallows the N7 panic log so it
// doesn't pollute go test output.
func TestSafeRun_ConvertsPanicToRecoveredValue(t *testing.T) {
	var rec any
	_ = captureStderr(t, func() {
		rec = safeRun("test", func() {
			panic("boom")
		})
	})
	if rec == nil {
		t.Fatal("expected non-nil recovered value")
	}
	if s, _ := rec.(string); s != "boom" {
		t.Errorf("recovered=%v, want \"boom\"", rec)
	}
}

// TestSafeRun_LogsPanicToStderr — N7 in audit ledger (Sprint 6). Every
// existing call site uses `_ = safeRun(...)`, so safeRun itself must log
// the panic or it's invisible to the operator.
func TestSafeRun_LogsPanicToStderr(t *testing.T) {
	out := captureStderr(t, func() {
		_ = safeRun("OnEvent panic test", func() {
			panic("kaboom")
		})
	})
	if !strings.Contains(out, "OnEvent panic test") {
		t.Errorf("stderr missing label: %q", out)
	}
	if !strings.Contains(out, "kaboom") {
		t.Errorf("stderr missing recovered value: %q", out)
	}
	if !strings.Contains(out, "panicked") {
		t.Errorf("stderr missing panic word: %q", out)
	}
}

// TestSafeRun_NoLogOnSuccess — happy path must not write to stderr.
func TestSafeRun_NoLogOnSuccess(t *testing.T) {
	out := captureStderr(t, func() {
		_ = safeRun("happy", func() {})
	})
	if out != "" {
		t.Errorf("unexpected stderr on success: %q", out)
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
