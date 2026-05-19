//go:build !windows

package lsp

import (
	"os/exec"
	"syscall"
)

// setProcessGroup makes cmd start in its own process group so killProcessTree
// can SIGTERM every descendant at once. H13 parity (audit ledger): LSP
// language servers spawn helpers (e.g. tsserver → tsc, gopls → builders);
// without Setpgid those helpers leak when Close kills the server.
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killProcessTree SIGTERMs the entire process group, falling back to just
// the leader if pgid isn't available.
func killProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err == nil && pgid > 0 {
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
		return
	}
	_ = cmd.Process.Kill()
}
