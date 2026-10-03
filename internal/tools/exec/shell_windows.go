//go:build windows

package exec

import (
	"os/exec"
	"strings"
	"syscall"

	"github.com/orchestra/orchestra/internal/execshell"
)

// keepShellLine makes the shell run the line as it was written.
//
// cmd.exe: Go joins arguments with backslash-escaped quotes, a convention
// cmd.exe does not follow — `node -e "…"` arrived as `\"…\"` and broke.
// /s /c "<line>" makes it drop only the outer pair of quotes.
//
// Git Bash: MSYS turns an argument that looks like a POSIX path into a Windows
// one on its way to a native program — `cmd /c exit 3` ran `cmd C:/ exit 3`.
// MSYS_NO_PATHCONV hands every argument over as written.
func keepShellLine(cmd *exec.Cmd, viaShell bool) {
	if !viaShell || len(cmd.Args) != 3 {
		return
	}
	if strings.EqualFold(cmd.Args[0], "cmd") {
		if cmd.SysProcAttr == nil {
			cmd.SysProcAttr = &syscall.SysProcAttr{}
		}
		cmd.SysProcAttr.CmdLine = `cmd /s /c "` + cmd.Args[2] + `"`
		return
	}
	if sh := execshell.Default(); sh.Kind == execshell.KindBash && cmd.Args[0] == sh.Path {
		cmd.Env = append(cmd.Env, "MSYS_NO_PATHCONV=1", "MSYS2_ARG_CONV_EXCL=*")
	}
}
