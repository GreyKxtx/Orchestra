//go:build !windows

package subproc

import (
	"os/exec"
	"syscall"
)

// SetProcessGroup puts cmd in its own process group so KillProcessTree
// can SIGTERM every descendant at once via syscall.Kill(-pgid). Without
// this, `npx → node` style chains (MCP servers) and `gopls → builder`
// chains (LSP servers) leak the grandchildren when we kill the leader.
//
// Call BEFORE cmd.Start. Originally H13 / N5 in the audit ledger;
// consolidated here in S2 (Sprint 6).
func SetProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// KillProcessTree SIGTERMs the entire process group, falling back to
// just the leader if pgid isn't available (e.g. Setpgid wasn't called
// before Start).
func KillProcessTree(cmd *exec.Cmd) {
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
