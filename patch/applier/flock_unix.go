//go:build !windows

package applier

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// acquireProjectLock takes an exclusive POSIX advisory file lock on
// `<project>/.orchestra/apply.lock`. The returned release closure
// unlocks + closes the file. Blocks until the lock is acquired (or the
// caller's process is killed). H9 in audit ledger.
//
// Cross-process safety: a second Orchestra apply against the same project
// will wait here until the first finishes, so concurrent .orchestra.bak
// writes can't clobber each other.
func acquireProjectLock(projectRoot string) (release func(), err error) {
	dir := projectRoot + string(os.PathSeparator) + ".orchestra"
	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
		return nil, fmt.Errorf("apply lock: mkdir %s: %w", dir, mkErr)
	}
	path := dir + string(os.PathSeparator) + "apply.lock"
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("apply lock: open %s: %w", path, err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("apply lock: flock %s: %w", path, err)
	}
	return func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		_ = f.Close()
	}, nil
}
