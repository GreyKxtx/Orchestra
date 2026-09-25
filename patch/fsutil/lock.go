package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// LockFile takes an exclusive lock named by path (a lock file, created if
// missing) and returns its release. It serialises writers both inside the
// process and across processes — two cores on one project (the VS Code
// extension and the TUI), parallel subagents in one — with an OS lock
// (flock / LockFileEx) that the OS drops if the holder dies, so a crash never
// leaves it held. It blocks until the lock is free.
//
// The in-process mutex comes first: OS locks are held by the open file, and
// taking the mutex keeps goroutines from each blocking an OS thread on it.
func LockFile(path string) (release func(), err error) {
	mu := processLock(path)
	mu.Lock()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		mu.Unlock()
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		mu.Unlock()
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	if err := lockOS(f); err != nil {
		_ = f.Close()
		mu.Unlock()
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			unlockOS(f)
			_ = f.Close()
			mu.Unlock()
		})
	}, nil
}

var (
	processLocksMu sync.Mutex
	processLocks   = map[string]*sync.Mutex{}
)

func processLock(path string) *sync.Mutex {
	key := filepath.Clean(path)
	if abs, err := filepath.Abs(key); err == nil {
		key = abs
	}
	processLocksMu.Lock()
	defer processLocksMu.Unlock()
	mu := processLocks[key]
	if mu == nil {
		mu = &sync.Mutex{}
		processLocks[key] = mu
	}
	return mu
}
