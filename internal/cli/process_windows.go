//go:build windows

package cli

import "syscall"

// isProcessAlive reports whether a process with the given PID is still running.
//
// The portable `proc.Signal(syscall.Signal(0))` idiom does NOT work here: Go's
// os.Process.Signal on Windows supports only Kill and returns an error for
// everything else, so it reports every process — including this one — as dead.
// Discovery-file cleanup keyed on that answer would delete a *live* server's
// file. Ask the OS instead.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	const processQueryLimitedInformation = 0x1000
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return false // no such process, or not ours to inspect
	}
	defer func() { _ = syscall.CloseHandle(h) }()

	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	// STILL_ACTIVE. A process that genuinely exited with code 259 is
	// misreported as alive; that is the standard trade-off for this API and it
	// errs toward keeping a discovery file rather than deleting a live one.
	const stillActive = 259
	return code == stillActive
}
