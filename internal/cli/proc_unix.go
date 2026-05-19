//go:build !windows

package cli

import (
	"os/exec"
	"syscall"
)

// setProcessGroup puts cmd in its own process group so killProcessTree
// can SIGTERM the whole tree at once. N5 in audit ledger (Sprint 6):
// `orchestra core` spawns MCP servers and LSP language servers, which
// in turn spawn their own helpers; without Setpgid those grandchildren
// leak when the via-core CoreChild is closed.
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killProcessTree SIGTERMs the entire process group, falling back to
// just the leader if pgid isn't available.
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
