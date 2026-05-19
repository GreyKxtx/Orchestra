//go:build windows

package cli

import (
	"os/exec"
	"strconv"
)

// setProcessGroup is a no-op on Windows; killProcessTree handles
// descendants via `taskkill /T`. N5 in audit ledger (Sprint 6).
func setProcessGroup(_ *exec.Cmd) {}

// killProcessTree terminates the core subprocess and every descendant
// via `taskkill /F /T /PID <pid>`. Without `/T`, MCP/LSP children
// spawned by `orchestra core` leak as orphans on Windows.
func killProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := strconv.Itoa(cmd.Process.Pid)
	_ = exec.Command("taskkill", "/F", "/T", "/PID", pid).Run()
}
