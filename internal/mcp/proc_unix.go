//go:build !windows

package mcp

import (
	"os/exec"
	"syscall"
)

// setProcessGroup makes cmd start in its own process group so killProcessTree
// can send a signal to every descendant at once via syscall.Kill(-pgid). H13
// in audit ledger: without this, `npx → node` style MCP servers leak the
// `node` child when we kill `npx`.
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killProcessTree sends SIGTERM to the entire process group, falling back to
// killing just the leader if the pgid isn't available.
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
