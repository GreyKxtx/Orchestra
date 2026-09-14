package tools_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
)

// git.worktree.list had no test naming it, and add/remove/prune were tested
// only from below, in internal/git, against a directory that is always the
// repository root. The model calls them through Runner.Call with project_root
// as the anchor, and project_root is not always the repository root: a
// service inside a monorepo, or a project opened from a linked worktree — the
// way this very branch is checked out.

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// gitRepoWith makes dir a repository with one commit containing files.
func gitRepoWith(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	requireGit(t)
	for rel, body := range files {
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.email", "eval@example.invalid")
	gitRun(t, dir, "config", "user.name", "eval")
	gitRun(t, dir, "config", "commit.gpgsign", "false")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "fixture")
}

type worktreeListing struct {
	Worktrees []struct {
		Path    string `json:"path"`
		Name    string `json:"name"`
		Managed bool   `json:"managed"`
	} `json:"worktrees"`
}

func listManaged(t *testing.T, r *tools.Runner) map[string]bool {
	t.Helper()
	raw := mustCall(t, r, "git.worktree.list", map[string]any{})
	var l worktreeListing
	if err := json.Unmarshal([]byte(raw), &l); err != nil {
		t.Fatalf("git.worktree.list did not answer with its documented shape: %v\n%s", err, raw)
	}
	out := map[string]bool{}
	for _, w := range l.Worktrees {
		if w.Managed {
			out[w.Name] = true
		}
	}
	return out
}

// The description promises managed entries carry their name and managed=true.
// That is the only way a model can find a worktree it made earlier in order to
// remove it, so add → list → remove → list is the contract.
func TestModelView_WorktreeAddIsVisibleToListAndGoneAfterRemove(t *testing.T) {
	root := t.TempDir()
	gitRepoWith(t, root, map[string]string{"main.go": "package main\n"})
	r, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })

	if got := listManaged(t, r); len(got) != 0 {
		t.Fatalf("a fresh repository already lists managed worktrees: %v", got)
	}
	mustCall(t, r, "git.worktree.add", map[string]any{"name": "spike"})
	if got := listManaged(t, r); !got["spike"] {
		t.Errorf("git.worktree.add succeeded and git.worktree.list does not show it as managed "+
			"by name, so the model cannot find it again: %v", got)
	}
	mustCall(t, r, "git.worktree.remove", map[string]any{"name": "spike"})
	if got := listManaged(t, r); got["spike"] {
		t.Errorf("git.worktree.remove succeeded and the worktree is still listed: %v", got)
	}
}

// Never write outside project_root — CLAUDE.md lists it with the binding
// safety invariants. AddWorktree anchors on MainRepoRoot, the repository's
// root, so a project that is a subdirectory of its repository gets a whole
// checkout and a registry file written above it.
func TestModelView_WorktreeAddWritesNothingOutsideTheProjectRoot(t *testing.T) {
	repo := t.TempDir()
	gitRepoWith(t, repo, map[string]string{
		"app/main.go":  "package main\n",
		"other/lib.go": "package other\n",
		"README.md":    "# monorepo\n",
	})
	projectRoot := filepath.Join(repo, "app")
	r, err := tools.NewRunner(projectRoot, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })

	out, addErr := call(t, r, "git.worktree.add", map[string]any{"name": "spike"})

	outside := filepath.Join(repo, ".orchestra")
	if _, statErr := os.Stat(outside); statErr == nil {
		t.Errorf("git.worktree.add, called with project_root=%s, wrote %s — above the project "+
			"root (call error: %v):\n%s", projectRoot, outside, addErr, out)
	}
	// Nothing on disk is not enough on its own. The first version of this test
	// passed because add failed for an unrelated reason — MainRepoRoot resolved
	// to the repository's PARENT — so the refusal has to be the one that says why.
	if addErr == nil || !strings.Contains(addErr.Error(), "outside this project's root") {
		t.Errorf("add must refuse because the repository root is outside the project root, "+
			"not for any other reason:\n%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(repo), ".orchestra")); statErr == nil {
		t.Errorf("a .orchestra directory was created above the repository itself")
	}
}
