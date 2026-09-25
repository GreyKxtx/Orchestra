package exec

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMaybeShellExec_PlainCommandPassesThrough(t *testing.T) {
	cmd, args, viaShell, err := MaybeShellExec("git", []string{"status"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if viaShell {
		t.Error("plain command should not be routed via shell")
	}
	if cmd != "git" || len(args) != 1 || args[0] != "status" {
		t.Errorf("got cmd=%q args=%v", cmd, args)
	}
}

func TestMaybeShellExec_CommandWithSpacesGoesToShell(t *testing.T) {
	cmd, args, viaShell, err := MaybeShellExec("go version", nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !viaShell {
		t.Fatal("command with space should be routed via shell")
	}
	if cmd != "cmd" && cmd != "sh" {
		t.Errorf("unexpected shell name: %q", cmd)
	}
	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %v", args)
	}
	if args[1] != "go version" {
		t.Errorf("full command line not preserved: %v", args)
	}
}

func TestMaybeShellExec_CompoundCommandGoesToShell(t *testing.T) {
	_, args, viaShell, err := MaybeShellExec("go build && go test", nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !viaShell {
		t.Fatal("compound command should be routed via shell")
	}
	if args[1] != "go build && go test" {
		t.Errorf("compound preserved? got %q", args[1])
	}
}

func TestMaybeShellExec_RefusesArgsWhenShellRouting(t *testing.T) {
	_, _, _, err := MaybeShellExec("git log", []string{"$(rm -rf ~)"})
	if err == nil {
		t.Fatal("expected error when shell-routing with non-empty args")
	}
}

// A command the model runs does not see the core's provider keys.
func TestRunScrubsSecretsFromTheEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses env(1)")
	}
	t.Setenv("ORCH_TEST_PROVIDER_API_KEY", "sk-must-not-leak")
	t.Setenv("ORCH_TEST_PLAIN", "visible")
	resp, err := Run(context.Background(), t.TempDir(), 10*time.Second, 64*1024, RunRequest{Command: "env"})
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	if strings.Contains(resp.Stdout, "sk-must-not-leak") {
		t.Fatal("a secret-looking variable reached the command")
	}
	if !strings.Contains(resp.Stdout, "ORCH_TEST_PLAIN=visible") {
		t.Fatal("ordinary variables must still be passed")
	}
}
