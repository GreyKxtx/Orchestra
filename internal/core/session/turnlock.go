package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/orchestra/orchestra/patch/fsutil"
)

// Turn lock — a cross-process "turn in flight" marker (resilience audit,
// detach follow-up). A core process that starts a session turn drops
// .orchestra/sessions/<id>.turn.lock with its pid; a second core process
// (e.g. a reloaded VS Code window while the detached old core is still
// finishing the turn) sees the lock and refuses to start a concurrent turn
// on the same session — otherwise two agents would burn tokens in parallel
// and race on the same snapshot file.
//
// The lock is advisory: filesystem errors never block a turn, and stale
// locks (holder died, or older than maxTurnLockAge) are reclaimed.

// maxTurnLockAge caps how long a lock is honoured even when the holder pid
// looks alive — guards against PID reuse after a crash.
var maxTurnLockAge = 2 * time.Hour

// TurnLockInfo is the persisted lock payload.
type TurnLockInfo struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
}

// TurnActiveError reports that another turn holds the session lock.
type TurnActiveError struct {
	PID int
}

func (e *TurnActiveError) Error() string {
	return fmt.Sprintf("session turn already running (pid %d)", e.PID)
}

func turnLockPath(workspaceRoot, id string) string {
	return filepath.Join(workspaceRoot, ".orchestra", "sessions", id+".turn.lock")
}

// readTurnLock returns the parsed lock or nil when absent/corrupt.
func readTurnLock(workspaceRoot, id string) *TurnLockInfo {
	data, err := os.ReadFile(turnLockPath(workspaceRoot, id))
	if err != nil {
		return nil
	}
	var info TurnLockInfo
	if json.Unmarshal(data, &info) != nil || info.PID <= 0 {
		// Corrupt lock (torn write / manual edit) — treat as stale.
		return &TurnLockInfo{PID: -1}
	}
	return &info
}

// pidAlive reports whether pid refers to a running process. On Windows
// os.FindProcess fails for dead pids; on Unix it always succeeds and
// signal 0 probes liveness.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return p.Signal(syscall.Signal(0)) == nil
}

// lockStale reports whether an existing lock can be reclaimed.
func lockStale(info *TurnLockInfo) bool {
	if info == nil {
		return true
	}
	if !pidAlive(info.PID) {
		return true
	}
	if !info.StartedAt.IsZero() && time.Since(info.StartedAt) > maxTurnLockAge {
		return true
	}
	return false
}

// CheckTurnLock returns the current lock (nil when absent) and whether it is
// stale (holder dead / too old) and can be reclaimed.
func CheckTurnLock(workspaceRoot, id string) (info *TurnLockInfo, stale bool) {
	if workspaceRoot == "" || id == "" {
		return nil, false
	}
	info = readTurnLock(workspaceRoot, id)
	if info == nil {
		return nil, false
	}
	return info, lockStale(info)
}

// ClearStaleTurnLock removes the lock file when it is stale. Returns true
// when a stale lock was removed.
func ClearStaleTurnLock(workspaceRoot, id string) bool {
	info, stale := CheckTurnLock(workspaceRoot, id)
	if info == nil || !stale {
		return false
	}
	_ = os.Remove(turnLockPath(workspaceRoot, id))
	return true
}

// AcquireTurnLock claims the session turn lock for this process. On success
// the returned release func removes the lock (call it via defer at turn end).
// When another live process holds the lock, a *TurnActiveError is returned.
// Filesystem write failures are returned as plain errors — callers should
// treat them as non-fatal (the lock is advisory).
func AcquireTurnLock(workspaceRoot, id string) (release func(), err error) {
	if workspaceRoot == "" || id == "" {
		return func() {}, nil
	}
	if existing := readTurnLock(workspaceRoot, id); existing != nil && !lockStale(existing) {
		return nil, &TurnActiveError{PID: existing.PID}
	}
	info := TurnLockInfo{PID: os.Getpid(), StartedAt: time.Now().UTC()}
	data, merr := json.Marshal(info)
	if merr != nil {
		return nil, merr
	}
	path := turnLockPath(workspaceRoot, id)
	if werr := fsutil.AtomicWriteFile(path, data, 0o600); werr != nil {
		return nil, fmt.Errorf("turn lock write: %w", werr)
	}
	return func() { _ = os.Remove(path) }, nil
}
