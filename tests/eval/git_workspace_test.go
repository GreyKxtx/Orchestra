package eval

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitGitWorkspace_LeavesACleanRepositoryHoldingTheTaskFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := initGitWorkspace(context.Background(), root); err != nil {
		t.Fatalf("initGitWorkspace: %v", err)
	}

	// Clean, so a git.status the agent runs later describes the agent's work
	// and not the setup's leftovers.
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v: %s", err, out)
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Errorf("the workspace starts dirty, so a task cannot tell setup from agent:\n%s", out)
	}

	// And the file is in the commit, not merely on disk.
	cmd = exec.Command("git", "ls-files")
	cmd.Dir = root
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git ls-files: %v: %s", err, out)
	}
	if !strings.Contains(string(out), "main.go") {
		t.Errorf("the task's files are not in the commit:\n%s", out)
	}
}
