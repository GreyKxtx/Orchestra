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

// orchestra init writes a .gitignore block whose comment says what it is for:
// "Secrets can live in .orchestra.local.yml and runtime logs under .orchestra/,
// so a bare `git add .` after init must not be able to commit them." That
// protection lives only in the .gitignore init writes. A project nobody ran
// init in — a repo opened straight in the IDE, the eval workspace — has none,
// and git.commit with add: ["."] is the model's own path to committing the
// CKG database and the run logs. An earlier test in this file did exactly that
// by accident: "create mode 100644 .orchestra/ckg.db".
func TestModelView_GitCommitDoesNotCommitOrchestraRuntimeArtifacts(t *testing.T) {
	root := t.TempDir()
	gitRepoWith(t, root, map[string]string{"main.go": "package main\n"})
	r, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })

	write := func(rel, body string) {
		t.Helper()
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Runtime artifacts a real run leaves behind.
	write(".orchestra/llm_log.jsonl", `{"event":"request"}`+"\n")
	write(".orchestra/last_result.json", "{}\n")
	write(".orchestra/extra.db", "not really sqlite\n")
	write(".orchestra/plans/local/scratch.md", "# my local plan\n")
	// Project knowledge init deliberately keeps tracked.
	write(".orchestra/decisions.md", "# Decisions\n")
	write(".orchestra/plans/refactor.md", "# Plan\n")
	// The user's actual change.
	write("feature.go", "package main\n")

	out := mustCall(t, r, "git.commit", map[string]any{"message": "feature", "add": []string{"."}})
	committed := gitOut(t, root, "show", "--name-only", "--pretty=", "HEAD")
	t.Logf("git.commit answered %s\ncommitted:\n%s", out, committed)

	if !strings.Contains(committed, "feature.go") {
		t.Fatalf("the user's own change was not committed:\n%s", committed)
	}
	for _, artifact := range []string{"llm_log.jsonl", "last_result.json", ".db", "plans/local/"} {
		if strings.Contains(committed, artifact) {
			t.Errorf("git.commit add . committed the Orchestra runtime artifact %q — the thing "+
				"init's .gitignore block exists to prevent:\n%s", artifact, committed)
		}
	}
	for _, knowledge := range []string{".orchestra/decisions.md", ".orchestra/plans/refactor.md"} {
		if !strings.Contains(committed, knowledge) {
			t.Errorf("project knowledge %s, which init keeps tracked, was left out:\n%s", knowledge, committed)
		}
	}
}

// The same rule from a project inside a monorepo, plus the three things the
// filtered add must not lose: the user's own .gitignore, a change to a file
// that is already tracked, and the secrets file init's block names first.
//
// The subdirectory is the case that decides the implementation. git ls-files
// --exclude-from anchors a pattern with a slash at the top of the REPOSITORY,
// while init's block is read from the PROJECT root's .gitignore — so an
// unanchored ".orchestra/*" silently matched nothing from services/api.
func TestModelView_GitCommitFiltersArtifactsFromAMonorepoSubdirectoryToo(t *testing.T) {
	repo := t.TempDir()
	gitRepoWith(t, repo, map[string]string{
		"services/api/main.go":    "package main\n",
		"services/api/.gitignore": "secret.env\n",
	})
	projectRoot := filepath.Join(repo, "services", "api")
	r, err := tools.NewRunner(projectRoot, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })

	write := func(rel, body string) {
		t.Helper()
		abs := filepath.Join(projectRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".orchestra.local.yml", "llm:\n  api_key: sk-not-a-real-key\n")
	write(".orchestra/llm_log.jsonl", "{}\n")
	write(".orchestra/decisions.md", "# Decisions\n")
	write("secret.env", "TOKEN=1\n")
	write("main.go", "package main\n\nfunc main() {}\n") // a tracked file, modified
	write("handler.go", "package main\n")

	mustCall(t, r, "git.commit", map[string]any{"message": "api change", "add": []string{"."}})
	committed := gitOut(t, repo, "show", "--name-only", "--pretty=", "HEAD")

	for _, want := range []string{"services/api/main.go", "services/api/handler.go", "services/api/.orchestra/decisions.md"} {
		if !strings.Contains(committed, want) {
			t.Errorf("%s should have been committed:\n%s", want, committed)
		}
	}
	for _, never := range []string{".orchestra.local.yml", "llm_log.jsonl", "secret.env"} {
		if strings.Contains(committed, never) {
			t.Errorf("%s was committed from a monorepo subdirectory:\n%s", never, committed)
		}
	}
}
