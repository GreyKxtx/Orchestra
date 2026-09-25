//go:build !windows

package fsutil

import (
	"os"

	"golang.org/x/sys/unix"
)

func lockOS(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_EX) }

func unlockOS(f *os.File) { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN) }
