package config

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/orchestra/orchestra/patch/fsutil"
)

// Cross-process write lock for .orchestra.yml. Two core processes can serve
// the same project at once (TUI in a terminal + the VS Code extension); both
// mutate the shared config through load→modify→Save. Without a lock the
// slower writer silently reverts the faster one's changes. The lock
// serialises writers across processes; in-process serialisation is handled
// by the callers' mutexes.
//
// Best effort by design: the lock lives next to the config as
// .orchestra.yml.lock, is held only for the duration of one Save, and locks
// older than staleLockAge are reclaimed (crashed writer).

const (
	lockAcquireTimeout = 2 * time.Second
	lockRetryInterval  = 25 * time.Millisecond
	staleLockAge       = 10 * time.Second
)

// acquireFileLock claims path.lock via O_CREATE|O_EXCL. Returns an unlock
// func. On timeout it steals the lock (a config write takes milliseconds —
// anything older is a crashed writer) so a leaked lock can never brick
// config saves.
func acquireFileLock(path string) func() {
	lockPath := path + ".lock"
	deadline := time.Now().Add(lockAcquireTimeout)
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(f, "%s", strconv.Itoa(os.Getpid()))
			_ = f.Close()
			return func() { _ = os.Remove(lockPath) }
		}
		if info, serr := os.Stat(lockPath); serr == nil && time.Since(info.ModTime()) > staleLockAge {
			// Crashed writer left the lock behind — reclaim it.
			_ = os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			// Never block a save forever: steal and proceed. Losing this
			// race is strictly better than failing to persist settings.
			_ = os.Remove(lockPath)
			continue
		}
		time.Sleep(lockRetryInterval)
	}
}

// UpdateFile rewrites the config file at path under its lock: fn gets the
// bytes on disk and returns the bytes to write, which land atomically. A
// command that edits one key of .orchestra.yml (orchestra model) goes
// through it, so it neither races a Save from another process nor leaves a
// torn file behind (ARCH-11: it used to os.WriteFile the result straight
// over the config).
func UpdateFile(path string, fn func(current []byte) ([]byte, error)) error {
	unlock := acquireFileLock(path)
	defer unlock()
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	next, err := fn(current)
	if err != nil {
		return err
	}
	if bytes.Equal(next, current) {
		return nil
	}
	if err := fsutil.AtomicWriteFile(path, next, 0o600); err != nil {
		return fmt.Errorf("failed to replace config file: %w", err)
	}
	return nil
}
