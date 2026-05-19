//go:build windows

package mcp

import (
	"os/exec"
	"strconv"
)

// setProcessGroup is a no-op on Windows — we don't need a fancy process-group
// setup. Cleanup of the descendant tree is done in killProcessTree via
// `taskkill /T`, which walks the child-process snapshot the OS already keeps.
// H13 in audit ledger.
func setProcessGroup(_ *exec.Cmd) {}

// killProcessTree terminates the process and every descendant via
// `taskkill /F /T /PID <pid>`. Without this, `npx → node` style MCP
// servers leave the `node` child running as an orphan when we kill `npx`.
//
// The implementation deliberately avoids `golang.org/x/sys/windows` so we
// don't add a new dependency just for `JobObject`; taskkill is present on
// every supported Windows host (Win7+) and `/T` does the tree walk for us.
func killProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := strconv.Itoa(cmd.Process.Pid)
	_ = exec.Command("taskkill", "/F", "/T", "/PID", pid).Run()
}
