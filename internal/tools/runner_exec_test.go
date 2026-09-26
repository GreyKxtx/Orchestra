package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestExecRun_Helper is invoked as a subprocess by exec integration tests in
// internal/tools/exec and by this dry-run delegate test.
func TestExecRun_Helper(t *testing.T) {
	mode := os.Getenv("ORCHESTRA_EXEC_HELPER_MODE")
	if mode == "" {
		return
	}
	switch mode {
	case "spam":
		fmt.Print(strings.Repeat("a", 200_000))
	case "sleep":
		time.Sleep(500 * time.Millisecond)
	case "cat-file":
		// Prints the file ORCHESTRA_EXEC_HELPER_FILE names, relative to the
		// working directory: what the command sees.
		b, err := os.ReadFile(os.Getenv("ORCHESTRA_EXEC_HELPER_FILE"))
		if err != nil {
			fmt.Print("ERR " + err.Error())
			return
		}
		fmt.Print(string(b))
	case "write-file":
		// Writes ORCHESTRA_EXEC_HELPER_FILE in the working directory: what
		// a command leaves behind.
		_ = os.WriteFile(os.Getenv("ORCHESTRA_EXEC_HELPER_FILE"), []byte("from command\n"), 0o644)
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
