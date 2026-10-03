package exec

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A command line with quotes inside reaches the shell as the model wrote it.
// Go quotes a single argument with backslash escapes, which cmd.exe does not
// read: live, every `node -e "…"` a model wrote failed with "Unterminated
// string constant", and the model spent its steps rewriting the check.
func TestRun_QuotesInsideAShellLineSurvive(t *testing.T) {
	t.Setenv("ORCHESTRA_EXEC_HELPER_MODE", "args")
	line := `"` + os.Args[0] + `" -test.run=TestExecRun_Helper$ "x y" plain`
	resp, err := Run(context.Background(), t.TempDir(), 30*time.Second, 100*1024, RunRequest{
		Command: line, Workdir: ".", TimeoutMS: 30_000,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(resp.Stdout, "ARGS:x y|plain") {
		t.Fatalf("the program got the wrong arguments: stdout %q stderr %q exit %d", resp.Stdout, resp.Stderr, resp.ExitCode)
	}
}

// Git Bash turns an argument that looks like a POSIX path into a Windows one
// on its way to a native program: `cmd /c exit 3` ran `cmd C:/ exit 3`, which
// exited 0, and an acceptance check that should have failed passed.
func TestRun_ArgumentsReachANativeProgramAsWritten(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("cmd.exe is Windows'")
	}
	resp, err := Run(context.Background(), t.TempDir(), 30*time.Second, 100*1024, RunRequest{
		Command: "cmd /c exit 3", Workdir: ".", TimeoutMS: 30_000,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp.ExitCode != 3 {
		t.Fatalf("exit %d, want 3 (stdout %q stderr %q)", resp.ExitCode, resp.Stdout, resp.Stderr)
	}
}
