package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initGitRepo initialises a git repo in root with a single initial commit.
// It also adds a .gitignore that ignores .orchestra/ so NewRunner's artifact
// directory does not appear as an untracked file.
func initGitRepo(t *testing.T, root string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".orchestra/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "readme.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".gitignore", "readme.txt")
	run("commit", "-m", "initial commit")
}

func newGitRunner(t *testing.T) (*Runner, string) {
	t.Helper()
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r, root
}

func skipIfNoGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
}

func TestGitStatus_CleanRepo(t *testing.T) {
	skipIfNoGit(t)
	r, root := newGitRunner(t)
	initGitRepo(t, root)

	resp, err := r.GitStatus(context.Background(), GitStatusRequest{})
	if err != nil {
		t.Fatalf("GitStatus: %v", err)
	}
	if !resp.Clean {
		t.Errorf("expected clean repo, got output: %q", resp.Output)
	}
}

func TestGitStatus_DirtyRepo(t *testing.T) {
	skipIfNoGit(t)
	r, root := newGitRunner(t)
	initGitRepo(t, root)

	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := r.GitStatus(context.Background(), GitStatusRequest{})
	if err != nil {
		t.Fatalf("GitStatus: %v", err)
	}
	if resp.Clean {
		t.Error("expected dirty repo")
	}
	if !strings.Contains(resp.Output, "new.txt") {
		t.Errorf("expected new.txt in status output, got: %q", resp.Output)
	}
}

func TestGitStatus_ReturnsCurrentBranch(t *testing.T) {
	skipIfNoGit(t)
	r, root := newGitRunner(t)
	initGitRepo(t, root)

	resp, err := r.GitStatus(context.Background(), GitStatusRequest{})
	if err != nil {
		t.Fatalf("GitStatus: %v", err)
	}
	if resp.Branch != "main" {
		t.Errorf("expected branch 'main', got %q", resp.Branch)
	}
}

func TestGitLog_Basic(t *testing.T) {
	skipIfNoGit(t)
	r, root := newGitRunner(t)
	initGitRepo(t, root)

	resp, err := r.GitLog(context.Background(), GitLogRequest{N: 5})
	if err != nil {
		t.Fatalf("GitLog: %v", err)
	}
	if !strings.Contains(resp.Output, "initial commit") {
		t.Errorf("expected 'initial commit' in log, got: %q", resp.Output)
	}
}

func TestGitLog_Oneline(t *testing.T) {
	skipIfNoGit(t)
	r, root := newGitRunner(t)
	initGitRepo(t, root)

	resp, err := r.GitLog(context.Background(), GitLogRequest{N: 5, Oneline: true})
	if err != nil {
		t.Fatalf("GitLog oneline: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(resp.Output), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 line in oneline log, got %d: %q", len(lines), resp.Output)
	}
}

func TestGitLog_InvalidRef(t *testing.T) {
	skipIfNoGit(t)
	r, root := newGitRunner(t)
	initGitRepo(t, root)

	_, err := r.GitLog(context.Background(), GitLogRequest{Ref: "../../etc/passwd"})
	if err == nil {
		t.Fatal("expected error for invalid ref")
	}
}

func TestGitSafeRef_RejectsLeadingDash(t *testing.T) {
	bad := []string{"-n5", "--all", "-p", "--"}
	for _, s := range bad {
		if isGitSafeRef(s) {
			t.Errorf("isGitSafeRef(%q) = true, want false", s)
		}
	}
	good := []string{"main", "origin/main", "v1.0.0", "HEAD~1", "abc123"}
	for _, s := range good {
		if !isGitSafeRef(s) {
			t.Errorf("isGitSafeRef(%q) = false, want true", s)
		}
	}
}

func TestGitStatus_EmptyRepo(t *testing.T) {
	skipIfNoGit(t)
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		out, execErr := cmd.CombinedOutput()
		if execErr != nil {
			t.Fatalf("git %v: %v\n%s", args, execErr, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")

	resp, statusErr := r.GitStatus(context.Background(), GitStatusRequest{})
	if statusErr != nil {
		t.Fatalf("GitStatus on empty repo: %v", statusErr)
	}
	if resp.Branch != "main" {
		t.Errorf("empty repo branch = %q, want %q", resp.Branch, "main")
	}
}
