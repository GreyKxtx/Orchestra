//go:build windows

package subproc

import (
	"os/exec"
	"strconv"
)

// SetProcessGroup is a no-op on Windows. Tree cleanup is done in
// KillProcessTree via `taskkill /T`, which walks the child-process
// snapshot the OS already keeps. Consolidated here in S2 (audit
// ledger, Sprint 6).
func SetProcessGroup(_ *exec.Cmd) {}

// KillProcessTree terminates the leader and every descendant via
// `taskkill /F /T /PID <pid>`. Without `/T`, helpers spawned by the
// subprocess (npx → node, gopls → builder, etc.) leak as orphans.
//
// We deliberately shell out to taskkill rather than depend on
// golang.org/x/sys/windows JobObjects: taskkill is present on every
// supported Windows host (Win7+) and `/T` does the snapshot walk for
// us.
func KillProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := strconv.Itoa(cmd.Process.Pid)
	_ = exec.Command("taskkill", "/F", "/T", "/PID", pid).Run()
}
