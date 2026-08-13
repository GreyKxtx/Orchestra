package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestTurnLock_AcquireReleaseCycle: the happy path — acquire marks the
// session, a second acquire is refused, release frees it again.
func TestTurnLock_AcquireReleaseCycle(t *testing.T) {
	dir := t.TempDir()
	id := "sess-lock-1"

	release, err := AcquireTurnLock(dir, id)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	info, stale := CheckTurnLock(dir, id)
	if info == nil || stale {
		t.Fatalf("expected live lock, got info=%v stale=%v", info, stale)
	}
	if info.PID != os.Getpid() {
		t.Fatalf("lock pid = %d, want %d", info.PID, os.Getpid())
	}

	// Second acquire while held must fail with TurnActiveError.
	if _, err := AcquireTurnLock(dir, id); err == nil {
		t.Fatal("second acquire should fail")
	} else {
		var active *TurnActiveError
		if !errors.As(err, &active) {
			t.Fatalf("want TurnActiveError, got %T: %v", err, err)
		}
		if active.PID != os.Getpid() {
			t.Fatalf("active pid = %d, want %d", active.PID, os.Getpid())
		}
	}

	release()
	if info, _ := CheckTurnLock(dir, id); info != nil {
		t.Fatalf("lock should be gone after release, got %+v", info)
	}
	// Re-acquire after release works.
	release2, err := AcquireTurnLock(dir, id)
	if err != nil {
		t.Fatalf("re-acquire: %v", err)
	}
	release2()
}

// TestTurnLock_StaleHolderReclaimed: a lock whose holder pid is dead is
// stale — CheckTurnLock reports it, ClearStaleTurnLock removes it, and a
// new acquire succeeds (crash recovery path).
func TestTurnLock_StaleHolderReclaimed(t *testing.T) {
	dir := t.TempDir()
	id := "sess-lock-stale"
	writeLock(t, dir, id, TurnLockInfo{PID: 999_999_999, StartedAt: time.Now().UTC()})

	info, stale := CheckTurnLock(dir, id)
	if info == nil || !stale {
		t.Fatalf("expected stale lock, got info=%v stale=%v", info, stale)
	}
	if !ClearStaleTurnLock(dir, id) {
		t.Fatal("ClearStaleTurnLock should remove a stale lock")
	}
	if info, _ := CheckTurnLock(dir, id); info != nil {
		t.Fatalf("lock still present after clear: %+v", info)
	}

	// Stale lock must not block a new turn either.
	writeLock(t, dir, id, TurnLockInfo{PID: 999_999_999, StartedAt: time.Now().UTC()})
	release, err := AcquireTurnLock(dir, id)
	if err != nil {
		t.Fatalf("acquire over stale lock: %v", err)
	}
	release()
}

// TestTurnLock_TooOldIsStale: even an alive holder is not honoured past
// maxTurnLockAge (PID reuse guard).
func TestTurnLock_TooOldIsStale(t *testing.T) {
	dir := t.TempDir()
	id := "sess-lock-old"
	writeLock(t, dir, id, TurnLockInfo{
		PID:       os.Getpid(), // definitely alive
		StartedAt: time.Now().UTC().Add(-3 * time.Hour),
	})
	if _, stale := CheckTurnLock(dir, id); !stale {
		t.Fatal("lock older than maxTurnLockAge must be stale")
	}
}

// TestTurnLock_CorruptFileIsStale: a torn/garbage lock file must never
// block turns forever.
func TestTurnLock_CorruptFileIsStale(t *testing.T) {
	dir := t.TempDir()
	id := "sess-lock-corrupt"
	path := turnLockPath(dir, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, stale := CheckTurnLock(dir, id)
	if info == nil || !stale {
		t.Fatalf("corrupt lock should be stale, got info=%v stale=%v", info, stale)
	}
	release, err := AcquireTurnLock(dir, id)
	if err != nil {
		t.Fatalf("acquire over corrupt lock: %v", err)
	}
	release()
}

func writeLock(t *testing.T, workspaceRoot, id string, info TurnLockInfo) {
	t.Helper()
	path := turnLockPath(workspaceRoot, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
