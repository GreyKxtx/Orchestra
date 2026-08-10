package exec

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
		fmt.Print(strings.Repeat("a", 200_000))
	case "sleep":
		time.Sleep(500 * time.Millisecond)
	}
}

func TestRun_OutputLimit_Truncates(t *testing.T) {
	root := t.TempDir()
	os.Setenv("ORCHESTRA_EXEC_HELPER_MODE", "spam")
	defer os.Unsetenv("ORCHESTRA_EXEC_HELPER_MODE")

	resp, err := Run(context.Background(), root, 30*time.Second, 100*1024, RunRequest{
		Command:       os.Args[0],
		Args:          []string{"-test.run=TestExecRun_Helper$"},
		Workdir:       ".",
		OutputLimitKB: 10,
		TimeoutMS:     30_000,
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if resp.ExitCode != 0 {
		t.Fatalf("unexpected exit code: %d", resp.ExitCode)
	}
	if !resp.Truncated {
		t.Fatal("expected truncated=true")
	}
	if len(resp.Stdout) > 10*1024 {
		t.Fatalf("stdout exceeds limit: %d", len(resp.Stdout))
	}
}

func TestRun_Timeout_ReturnsExecTimeout(t *testing.T) {
	root := t.TempDir()
	os.Setenv("ORCHESTRA_EXEC_HELPER_MODE", "sleep")
	defer os.Unsetenv("ORCHESTRA_EXEC_HELPER_MODE")

	_, err := Run(context.Background(), root, 30*time.Second, 100*1024, RunRequest{
		Command:   os.Args[0],
		Args:      []string{"-test.run=TestExecRun_Helper$"},
		Workdir:   ".",
		TimeoutMS: 50,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	coreErr, ok := protocol.AsError(err)
	if !ok {
		t.Fatalf("expected protocol.Error, got %T: %v", err, err)
	}
	if coreErr.Code != protocol.ExecTimeout {
		t.Fatalf("expected %s, got %s", protocol.ExecTimeout, coreErr.Code)
	}
}

func TestRun_WorkdirTraversal_ReturnsPathTraversal(t *testing.T) {
	root := t.TempDir()
	_, err := Run(context.Background(), root, 30*time.Second, 100*1024, RunRequest{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestExecRun_Helper$"},
		Workdir: "..",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	coreErr, ok := protocol.AsError(err)
	if !ok {
		t.Fatalf("expected protocol.Error, got %T: %v", err, err)
	}
	if coreErr.Code != protocol.PathTraversal {
		t.Fatalf("expected %s, got %s", protocol.PathTraversal, coreErr.Code)
	}
}
