package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/protocol"
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

