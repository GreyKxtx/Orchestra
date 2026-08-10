package exec

import (
	"testing"
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
