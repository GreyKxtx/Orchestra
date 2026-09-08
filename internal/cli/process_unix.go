//go:build !windows

package cli

import (
	"os"
	"syscall"
)

// isProcessAlive reports whether a process with the given PID is still running.
// Returns false if the process doesn't exist or if we can't determine its state.
//
// Signal(0) checks for existence without delivering anything.
func isProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
