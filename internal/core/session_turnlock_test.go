package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	coresession "github.com/orchestra/orchestra/internal/core/session"
)

func newTurnLockTestCore(t *testing.T) (*Core, string) {
	t.Helper()
	root := t.TempDir()
	cfg := config.DefaultConfig(root)
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	c, err := New(root, Options{})
	if err != nil {
		t.Fatalf("new core: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, root
}

// TestSessionMessage_RefusesWhenTurnLockHeld: while the on-disk turn lock is
// held by a live process, a second session.message on the same session must
// be rejected instead of running a parallel (double-paid) turn.
func TestSessionMessage_RefusesWhenTurnLockHeld(t *testing.T) {
	c, root := newTurnLockTestCore(t)
	start, err := c.SessionStart(SessionStartParams{})
	if err != nil {
		t.Fatalf("session start: %v", err)
	}

	release, err := coresession.AcquireTurnLock(root, start.SessionID)
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	defer release()

	_, err = c.SessionMessage(context.Background(), SessionMessageParams{
		SessionID: start.SessionID,
		Content:   "hi",
	})
	if err == nil {
		t.Fatal("session.message should be refused while the turn lock is held")
	}
	if !strings.Contains(err.Error(), "busy") && !strings.Contains(err.Error(), "завершается") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestSessionGet_ReportsInterruptedAndClearsStaleLock: a lock left behind by
// a crashed core (dead pid) is surfaced once as interrupted and reclaimed so
// the next turn can start.
func TestSessionGet_ReportsInterruptedAndClearsStaleLock(t *testing.T) {
	c, root := newTurnLockTestCore(t)
	start, err := c.SessionStart(SessionStartParams{})
	if err != nil {
		t.Fatalf("session start: %v", err)
	}

	lockPath := filepath.Join(root, ".orchestra", "sessions", start.SessionID+".turn.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte(`{"pid":999999999,"started_at":"2020-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := c.SessionGet(SessionGetParams{SessionID: start.SessionID})
	if err != nil {
		t.Fatalf("session.get: %v", err)
	}
	if !res.Interrupted {
		t.Fatal("expected interrupted=true for stale turn lock")
	}
	if res.ExternalTurn {
		t.Fatal("stale lock must not be reported as an external turn")
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("stale lock should be reclaimed, stat err=%v", err)
	}

	// Second call: lock is gone, no flags.
	res2, err := c.SessionGet(SessionGetParams{SessionID: start.SessionID})
	if err != nil {
		t.Fatalf("session.get #2: %v", err)
	}
	if res2.Interrupted || res2.ExternalTurn {
		t.Fatalf("flags should clear after reclaim: %+v", res2)
	}
}
