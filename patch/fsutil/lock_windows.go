//go:build windows

package fsutil

import (
	"os"

	"golang.org/x/sys/windows"
)

// The whole file: offset 0, length max (Windows convention). Without
// LOCKFILE_FAIL_IMMEDIATELY the call blocks until the lock is free.
func lockOS(f *os.File) error {
	var ol windows.Overlapped
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 0xFFFFFFFF, 0xFFFFFFFF, &ol)
}

func unlockOS(f *os.File) {
	var ol windows.Overlapped
	_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 0xFFFFFFFF, 0xFFFFFFFF, &ol)
}
