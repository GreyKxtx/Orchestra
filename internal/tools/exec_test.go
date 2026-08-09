package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/orchestra/orchestra/protocol"
)

func TestExecRun_Helper(t *testing.T) {
	mode := os.Getenv("ORCHESTRA_EXEC_HELPER_MODE")
	if mode == "" {
		return
	}
	switch mode {
	case "spam":
		// Produce > 100KB output.
		fmt.Print(strings.Repeat("a", 200_000))
	case "sleep":
		time.Sleep(500 * time.Millisecond)
	default:
		// Unknown mode: do nothing.
	}
}

func TestExecRun_OutputLimit_Truncates(t *testing.T) {
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner failed: %v", err)
	}
	t.Cleanup(func() { r.Close() })

	os.Setenv("ORCHESTRA_EXEC_HELPER_MODE", "spam")
	defer os.Unsetenv("ORCHESTRA_EXEC_HELPER_MODE")

	resp, err := r.ExecRun(context.Background(), ExecRunRequest{
		Command:       os.Args[0],
		Args:          []string{"-test.run=TestExecRun_Helper$"},
		Workdir:       ".",
		OutputLimitKB: 10,
		TimeoutMS:     30_000,
	})
	if err != nil {
		t.Fatalf("ExecRun failed: %v", err)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("unexpected exit code: %d", resp.ExitCode)
	}
	if !resp.Truncated {
		t.Fatalf("expected truncated=true")
	}
	if len(resp.Stdout) > 10*1024 {
		t.Fatalf("stdout exceeds limit: %d", len(resp.Stdout))
	}
}

func TestExecRun_Timeout_ReturnsExecTimeout(t *testing.T) {
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner failed: %v", err)
	}
	t.Cleanup(func() { r.Close() })

	os.Setenv("ORCHESTRA_EXEC_HELPER_MODE", "sleep")
	defer os.Unsetenv("ORCHESTRA_EXEC_HELPER_MODE")

	_, err = r.ExecRun(context.Background(), ExecRunRequest{
		Command:   os.Args[0],
		Args:      []string{"-test.run=TestExecRun_Helper$"},
		Workdir:   ".",
		TimeoutMS: 50,
	})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	coreErr, ok := protocol.AsError(err)
	if !ok {
		t.Fatalf("expected protocol.Error, got %T: %v", err, err)
	}
	if coreErr.Code != protocol.ExecTimeout {
		t.Fatalf("expected %s, got %s", protocol.ExecTimeout, coreErr.Code)
	}
}

func TestMaybeShellExec_PlainCommandPassesThrough(t *testing.T) {
	cmd, args, viaShell, err := maybeShellExec("git", []string{"status"})
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
	cmd, args, viaShell, err := maybeShellExec("go version", nil)
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
	_, args, viaShell, err := maybeShellExec("go build && go test", nil)
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

// Args supplied alongside a shell-requiring command must be rejected — splicing
// them into the shell expression would be an injection sink. The caller has to
// put the whole shell expression in `command`.
func TestMaybeShellExec_RefusesArgsWhenShellRouting(t *testing.T) {
	_, _, _, err := maybeShellExec("git log", []string{"$(rm -rf ~)"})
	if err == nil {
		t.Fatal("expected error when shell-routing with non-empty args")
	}
}

func TestExecRun_WorkdirTraversal_ReturnsPathTraversal(t *testing.T) {
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner failed: %v", err)
	}
	t.Cleanup(func() { r.Close() })

	_, err = r.ExecRun(context.Background(), ExecRunRequest{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestExecRun_Helper$"},
		Workdir: "..",
	})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	coreErr, ok := protocol.AsError(err)
	if !ok {
		t.Fatalf("expected protocol.Error, got %T: %v", err, err)
	}
	if coreErr.Code != protocol.PathTraversal {
		t.Fatalf("expected %s, got %s", protocol.PathTraversal, coreErr.Code)
	}
}

func TestExecRun_dryRunBlock_allowDespite(t *testing.T) {
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{BlockExecInDryRun: true, DryRun: true})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })

	req := ExecRunRequest{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestExecRun_Helper$", "-test.count=1"},
	}
	_, err = r.ExecRun(context.Background(), req)
	if err == nil {
		t.Fatal("expected dry-run block")
	}
	if !strings.Contains(err.Error(), "dry-run") {
		t.Fatalf("expected dry-run error, got: %v", err)
	}
	r.SetAllowExecDespiteDryRun(true)
	resp, err := r.ExecRun(context.Background(), req)
	if err != nil {
		t.Fatalf("allowDespite should unlock exec: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
}

