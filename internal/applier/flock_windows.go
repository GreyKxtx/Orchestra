//go:build windows

package applier

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// acquireProjectLock takes an exclusive Windows file lock via LockFileEx
// on `<project>\.orchestra\apply.lock`. Mirrors the POSIX flock variant
// (see flock_unix.go). H9 in audit ledger.
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

	// LOCKFILE_EXCLUSIVE_LOCK | (no LOCKFILE_FAIL_IMMEDIATELY → blocking).
	const flags = windows.LOCKFILE_EXCLUSIVE_LOCK
	// Lock the entire file. Offset/length all-zero is "this file" with the
	// max-uint64 region per Windows convention.
	var ol windows.Overlapped
	if err := windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 0xFFFFFFFF, 0xFFFFFFFF, &ol); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("apply lock: LockFileEx %s: %w", path, err)
	}
	return func() {
		_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 0xFFFFFFFF, 0xFFFFFFFF, &ol)
		_ = f.Close()
	}, nil
}
