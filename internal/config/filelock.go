package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
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
