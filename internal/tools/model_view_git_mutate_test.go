package tools_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
)

// git.commit, git.branch, git.checkout, git.push and gh.pr.create are gated on
// exec consent — the same consent bash needs — and the registry comment says
// why: "bash could run these commands anyway". That is true of consent and not
// of the dry-run preview. core sets BlockExecInDryRun and promises remote
// clients a preview with no side effects; bash honours it, and these did not
// look at it at all. In a preview turn with exec consent granted in config,
// the model could commit, switch branches under the user, and push — every one
// of them refused if it had written the same command in bash.
//
// git.status/log/diff and a plain git.branch listing only read, and stay
// available: a preview that cannot look at the repository is useless.

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
}

func previewRunner(t *testing.T, root string) *tools.Runner {
	t.Helper()
	r, err := tools.NewRunner(root, tools.RunnerOptions{BlockExecInDryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	r.SetDryRun(true)
	return r
}

func TestModelView_MutatingGitIsRefusedInADryRunPreviewLikeBashIs(t *testing.T) {
	root := t.TempDir()
	gitRepoWith(t, root, map[string]string{"main.go": "package main\n"})
	r := previewRunner(t, root)

	headBefore := gitOut(t, root, "rev-parse", "HEAD")
	branchBefore := gitOut(t, root, "rev-parse", "--abbrev-ref", "HEAD")

	// Control: bash refuses in this preview, and these tools are gated on the
	// same consent bash is.
	if _, err := call(t, r, "bash", map[string]any{"command": "git", "args": []string{"status"}}); err == nil {
		t.Fatal("bash ran in a blocked dry-run; the control is broken")
	}

	if err := os.WriteFile(filepath.Join(root, "new.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"git.commit", map[string]any{"message": "preview commit", "add": []string{"."}}},
		{"git.checkout", map[string]any{"new_branch": "preview-branch"}},
		{"git.branch", map[string]any{"create": "preview-created"}},
		{"git.push", map[string]any{"remote": "origin"}},
		{"gh.pr.create", map[string]any{"title": "preview", "body": "preview"}},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			out, err := call(t, r, tc.tool, tc.args)
			if err == nil {
				t.Fatalf("%s ran in a dry-run preview that refuses the same command in bash:\n%s", tc.tool, out)
			}
			if !strings.Contains(err.Error(), "dry-run") {
				t.Errorf("%s was refused for some other reason; the model cannot tell it to retry "+
					"with apply:true unless the refusal says dry-run:\n%s", tc.tool, err)
			}
		})
	}

	if got := gitOut(t, root, "rev-parse", "HEAD"); got != headBefore {
		t.Errorf("the repository gained a commit during a preview: %s → %s", headBefore, got)
	}
	if got := gitOut(t, root, "rev-parse", "--abbrev-ref", "HEAD"); got != branchBefore {
		t.Errorf("the branch changed under the user during a preview: %s → %s", branchBefore, got)
	}
	if got := gitOut(t, root, "branch", "--list"); strings.Contains(got, "preview-") {
		t.Errorf("a branch was created during a preview:\n%s", got)
	}
}

// The guard must gate the preview, not the tool: reading the repository stays
// available, and an applying run commits as before.
func TestModelView_ReadingGitAndApplyingRunsAreUnaffectedByThatGuard(t *testing.T) {
	root := t.TempDir()
	gitRepoWith(t, root, map[string]string{"main.go": "package main\n"})
	r := previewRunner(t, root)

	if out := mustCall(t, r, "git.status", map[string]any{}); out == "" {
		t.Error("git.status must keep working in a preview")
	}
	if out := mustCall(t, r, "git.branch", map[string]any{}); out == "" {
		t.Error("listing branches only reads and must keep working in a preview")
	}

	// An applying run is what --apply and the TUI do: the overlay commits to
	// disk, and shell work is unlocked.
	r.SetAllowExecDespiteDryRun(true)
	if err := os.WriteFile(filepath.Join(root, "new.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := call(t, r, "git.commit", map[string]any{"message": "applied commit", "add": []string{"."}})
	if err != nil {
		t.Fatalf("git.commit must still work once the run applies:\n%s", out)
	}
	if got := gitOut(t, root, "log", "-1", "--pretty=%s"); got != "applied commit" {
		t.Errorf("the commit did not land: %q", got)
	}
}

// --force is the one flag here that can destroy someone else's work. The tool
// turns it into --force-with-lease, which refuses when the remote moved since
// the last fetch. Nothing else pins that, and losing it would be invisible.
func TestModelView_GitPushForceIsForceWithLease(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin.git")
	requireGit(t)
	if out, err := exec.Command("git", "init", "--bare", "-q", origin).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	root := t.TempDir()
	gitRepoWith(t, root, map[string]string{"main.go": "package main\n"})
	gitRun(t, root, "remote", "add", "origin", origin)
	branch := gitOut(t, root, "rev-parse", "--abbrev-ref", "HEAD")

	r, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })

	mustCall(t, r, "git.push", map[string]any{"remote": "origin", "branch": branch, "set_upstream": true})

	// Someone else pushes a commit we have never seen.
	other := t.TempDir()
	if out, err := exec.Command("git", "clone", "-q", origin, other).CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v\n%s", err, out)
	}
	gitRun(t, other, "config", "user.email", "other@example.invalid")
	gitRun(t, other, "config", "user.name", "other")
	if err := os.WriteFile(filepath.Join(other, "theirs.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, other, "add", "-A")
	gitRun(t, other, "commit", "-q", "-m", "theirs")
	gitRun(t, other, "push", "-q", "origin", "HEAD:"+branch)

	// Our own new commit, then a force push. With --force this overwrites their
	// work; with --force-with-lease it is refused.
	if err := os.WriteFile(filepath.Join(root, "ours.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "ours")

	out, pushErr := call(t, r, "git.push", map[string]any{"remote": "origin", "branch": branch, "force": true})
	if pushErr == nil {
		t.Fatalf("force push overwrote a commit the run had never fetched; --force-with-lease "+
			"is what stops that:\n%s", out)
	}
	remoteHead := gitOut(t, origin, "log", "-1", "--pretty=%s", branch)
	if remoteHead != "theirs" {
		t.Errorf("the other person's commit is no longer the remote head: %q", remoteHead)
	}
}
