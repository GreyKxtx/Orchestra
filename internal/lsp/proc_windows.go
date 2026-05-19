//go:build windows

package lsp

import (
	"os/exec"
	"strconv"
)

// setProcessGroup is a no-op on Windows; killProcessTree handles descendants
// via `taskkill /T`. H13 parity (audit ledger).
func setProcessGroup(_ *exec.Cmd) {}

// killProcessTree terminates the LSP server and every descendant via
// `taskkill /F /T /PID <pid>`. Without `/T`, helpers spawned by the server
// (tsc, builder subprocess, etc.) leak as orphans on Windows.
func killProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := strconv.Itoa(cmd.Process.Pid)
	_ = exec.Command("taskkill", "/F", "/T", "/PID", pid).Run()
}
