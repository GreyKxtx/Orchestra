package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/git"
)

func writeWorktreeTestConfig(t *testing.T, dir string) {
	t.Helper()
	cfg := config.DefaultConfig(dir)
	if err := config.Save(filepath.Join(dir, ".orchestra.yml"), cfg); err != nil {
		t.Fatal(err)
	}
}

func initGitRepoCLI(t *testing.T, dir string) {
	t.Helper()
	runGitWorktreeCLI(t, dir, "init")
	runGitWorktreeCLI(t, dir, "config", "user.email", "t@t.com")
	runGitWorktreeCLI(t, dir, "config", "user.name", "T")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitWorktreeCLI(t, dir, "add", "README.md")
	runGitWorktreeCLI(t, dir, "commit", "-m", "init")
}

func runGitWorktreeCLI(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestWorktreeCLI_Roundtrip(t *testing.T) {
	root := t.TempDir()
	initGitRepoCLI(t, root)
	writeWorktreeTestConfig(t, root)
	origWD, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	worktreeForce = false
	worktreeRef = "HEAD"
	if err := runWorktreeAdd(nil, []string{"cli-wt"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	path, err := git.ResolveManagedWorktree(root, "cli-wt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat worktree: %v", err)
	}
	if err := runWorktreeRemove(nil, []string{"cli-wt"}); err != nil {
		t.Fatalf("remove: %v", err)
	}
}
