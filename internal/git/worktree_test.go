package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test User")
	writeFile(t, filepath.Join(dir, "README.md"), "# test\n")
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "init")
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMainRepoRoot(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	main, err := MainRepoRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(dir)
	if main != filepath.Clean(want) {
		t.Fatalf("main=%q want %q", main, want)
	}
}

func TestWorktreeAddListRemove(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, ".orchestra"), 0o755); err != nil {
		t.Fatal(err)
	}

	entry, err := AddWorktree(dir, "feat-a", "", "HEAD", false)
	if err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	if !entry.Managed || entry.Name != "feat-a" {
		t.Fatalf("entry: %+v", entry)
	}

	all, err := ListWorktrees(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("want 2 worktrees, got %d", len(all))
	}

	path, err := ResolveManagedWorktree(dir, "feat-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}

	if err := RemoveWorktree(dir, "feat-a", true); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	all, err = ListWorktrees(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("after remove want 1 worktree, got %d", len(all))
	}
}

func TestWorktreeDuplicateName(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	if _, err := AddWorktree(dir, "dup", "", "HEAD", false); err != nil {
		t.Fatal(err)
	}
	if _, err := AddWorktree(dir, "dup", "", "HEAD", false); err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestParseWorktreePorcelain(t *testing.T) {
	raw := `worktree /repo/main
HEAD abcdef1234567890
branch refs/heads/main

worktree /repo/.orchestra/worktrees/wt
HEAD 1111111111111111
branch refs/heads/orchestra/wt
`
	entries := parseWorktreePorcelain(raw)
	if len(entries) != 2 {
		t.Fatalf("got %d entries", len(entries))
	}
	if entries[0].Branch != "refs/heads/main" {
		t.Fatalf("branch0=%q", entries[0].Branch)
	}
	if entries[1].Path != "/repo/.orchestra/worktrees/wt" {
		t.Fatalf("path1=%q", entries[1].Path)
	}
}

func TestIsSafeWorktreeName(t *testing.T) {
	for _, tc := range []struct {
		name string
		ok   bool
	}{
		{"feat-a", true},
		{"my_worktree", true},
		{"", false},
		{"../evil", false},
		{"1bad", false},
	} {
		if got := IsSafeWorktreeName(tc.name); got != tc.ok {
			t.Errorf("IsSafeWorktreeName(%q)=%v want %v", tc.name, got, tc.ok)
		}
	}
}
