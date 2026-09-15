package git

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// gh.pr.view, gh.issue.list and gh.issue.view were three of the six tools no
// test in the repository named. They shell out to the gh CLI, which is why:
// the existing tests skip when gh is missing and assert nothing when it is
// present, since a real gh needs a real repository and a real token.
//
// A stub gh removes both problems. It records the arguments it was given and
// prints whatever the test wants, so the two halves that are ours — the
// command we build, and what we make of the answer — become testable, and the
// parts that are GitHub's stay out of it.
//
// These drive the Client directly rather than Runner.Call: for gh tools the
// dispatch layer is a pass-through (see toolDispatchTable), and the stub needs
// the unexported binary cache this package owns.

// fakeGH installs a stub gh that prints stdoutJSON and exits with code, and
// returns a function reading back the arguments it was called with.
func fakeGH(t *testing.T, stdoutJSON string, code int) func() []string {
	t.Helper()
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")

	var script, path string
	if runtime.GOOS == "windows" {
		path = filepath.Join(dir, "gh.cmd")
		script = "@echo off\r\n" +
			"echo %* > \"" + argsFile + "\"\r\n" +
			"echo " + stdoutJSON + "\r\n" +
			"exit /b " + itoa(code) + "\r\n"
	} else {
		path = filepath.Join(dir, "gh")
		script = "#!/bin/sh\n" +
			"echo \"$@\" > '" + argsFile + "'\n" +
			"cat <<'EOF'\n" + stdoutJSON + "\nEOF\n" +
			"exit " + itoa(code) + "\n"
	}
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	ghMu.Lock()
	prevBin, prevFound := ghBin, ghFound
	ghBin, ghFound = path, true
	ghMu.Unlock()
	t.Cleanup(func() {
		ghMu.Lock()
		ghBin, ghFound = prevBin, prevFound
		ghMu.Unlock()
	})

	return func() []string {
		raw, err := os.ReadFile(argsFile)
		if err != nil {
			t.Fatalf("the stub gh was never called: %v", err)
		}
		return strings.Fields(strings.TrimSpace(string(raw)))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

func hasArgPair(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

// The model's state and limit have to reach gh, or it gets open issues and
// twenty of them whatever it asked for — and has no way to tell.
func TestGHIssueList_PassesTheModelsFiltersToGH(t *testing.T) {
	readArgs := fakeGH(t, `[{"number":7,"title":"Crash on save","state":"CLOSED","author":{"login":"ann"},"url":"https://example.invalid/7","labels":[{"name":"bug"}],"updatedAt":"2026-01-01T00:00:00Z"}]`, 0)
	c := NewClient(t.TempDir())

	resp, err := c.GHIssueList(context.Background(), GHIssueListRequest{
		State: "closed", Limit: 3, Labels: []string{"bug"},
	})
	if err != nil {
		t.Fatalf("GHIssueList: %v", err)
	}

	args := readArgs()
	if !hasArgPair(args, "--state", "closed") {
		t.Errorf("the model asked for closed issues and gh was not told: %v", args)
	}
	if !hasArgPair(args, "--limit", "3") {
		t.Errorf("the model's limit did not reach gh: %v", args)
	}
	if !hasArgPair(args, "--label", "bug") {
		t.Errorf("the model's label filter did not reach gh: %v", args)
	}
	if len(resp.Issues) != 1 || resp.Issues[0].Number != 7 || resp.Issues[0].Title != "Crash on save" {
		t.Fatalf("the answer was not parsed into the documented shape: %+v", resp.Issues)
	}
	if resp.Issues[0].Author != "ann" {
		t.Errorf("author is nested in gh's JSON and must be flattened for the model: %+v", resp.Issues[0])
	}
}

// Comments are why a model views an issue rather than listing it: the
// discussion is where the actual requirement usually is.
func TestGHIssueView_KeepsTheDiscussionNotJustTheTitle(t *testing.T) {
	fakeGH(t, `{"number":7,"title":"Crash on save","body":"Steps: ...","state":"OPEN","url":"https://example.invalid/7","author":{"login":"ann"},"labels":[{"name":"bug"}],"comments":[{"author":{"login":"bob"},"body":"Only with autosave on","url":"https://example.invalid/7#1"}]}`, 0)
	c := NewClient(t.TempDir())

	resp, err := c.GHIssueView(context.Background(), GHIssueViewRequest{Number: 7})
	if err != nil {
		t.Fatalf("GHIssueView: %v", err)
	}
	if len(resp.Comments) != 1 || !strings.Contains(resp.Comments[0].Body, "autosave") {
		t.Fatalf("the comments were dropped; the model sees a title and no discussion: %+v", resp)
	}
	if resp.Comments[0].Author != "bob" {
		t.Errorf("comment author was not flattened: %+v", resp.Comments[0])
	}
	if len(resp.Labels) != 1 || resp.Labels[0] != "bug" {
		t.Errorf("labels were not flattened to names: %+v", resp.Labels)
	}
}

// gh fails for reasons the model can act on — not authenticated, no such PR,
// not a GitHub repo. If the tool answers with a bare failure, the model retries
// the same call; the reason has to travel.
func TestGHPRView_SaysWhatGHSaidWhenItFails(t *testing.T) {
	fakeGH(t, "", 1)
	c := NewClient(t.TempDir())

	_, err := c.GHPRView(context.Background(), GHPRViewRequest{Number: 42})
	if err == nil {
		t.Fatal("a gh failure came back as success")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "gh") {
		t.Errorf("the error does not name gh, so the model cannot tell whose failure this is: %v", err)
	}
}

// Without gh installed the answer must say so plainly: it is the one failure no
// retry and no rephrasing can fix.
func TestGHTools_SayGHIsMissingRatherThanFailingVaguely(t *testing.T) {
	ghMu.Lock()
	prevBin, prevFound := ghBin, ghFound
	ghBin, ghFound = "", false
	ghMu.Unlock()
	t.Cleanup(func() {
		ghMu.Lock()
		ghBin, ghFound = prevBin, prevFound
		ghMu.Unlock()
	})
	// Empty PATH so the re-lookup cannot find a real gh on this machine.
	t.Setenv("PATH", t.TempDir())

	c := NewClient(t.TempDir())
	_, err := c.GHIssueView(context.Background(), GHIssueViewRequest{Number: 1})
	if err == nil || !strings.Contains(err.Error(), "gh CLI not available") {
		t.Fatalf("a missing gh must be named as such, got: %v", err)
	}
}

// The negative answer must not stick: a process that started before gh was
// installed used to keep answering "not available" until it was restarted,
// because the lookup ran once behind a sync.Once.
func TestGHAvailable_NoticesGHAppearingLater(t *testing.T) {
	ghMu.Lock()
	prevBin, prevFound := ghBin, ghFound
	ghBin, ghFound = "", false
	ghMu.Unlock()
	t.Cleanup(func() {
		ghMu.Lock()
		ghBin, ghFound = prevBin, prevFound
		ghMu.Unlock()
	})

	empty := t.TempDir()
	t.Setenv("PATH", empty)
	if GHAvailable() {
		t.Fatal("gh reported available with an empty PATH")
	}

	name := "gh"
	if runtime.GOOS == "windows" {
		name = "gh.cmd"
	}
	if err := os.WriteFile(filepath.Join(empty, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !GHAvailable() {
		t.Error("gh was installed while the process ran and is still reported missing")
	}
}

// fakeGHScript installs a stub gh with a body of the test's choosing.
func fakeGHScript(t *testing.T, windowsBody, unixBody string) {
	t.Helper()
	dir := t.TempDir()
	path, script := filepath.Join(dir, "gh"), "#!/bin/sh\n"+unixBody
	if runtime.GOOS == "windows" {
		path, script = filepath.Join(dir, "gh.cmd"), "@echo off\r\n"+windowsBody
	}
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	ghMu.Lock()
	prevBin, prevFound := ghBin, ghFound
	ghBin, ghFound = path, true
	ghMu.Unlock()
	t.Cleanup(func() {
		ghMu.Lock()
		ghBin, ghFound = prevBin, prevFound
		ghMu.Unlock()
	})
}

// gh.pr.create asked gh for `pr create ... --json number,url,title`. gh pr
// create has no --json flag — list and view do, create does not. Run against
// the real gh 2.97.0 in an empty directory, that exact argv answers
// "unknown flag: --json" and exits 1 during flag parsing, before any network
// or repository access. So the tool could not create a pull request under any
// circumstances, and nothing noticed: the existing tests check an empty title
// and skip when gh is absent.
//
// This stub does what the real gh was observed to do: reject --json on
// pr create, and otherwise print the new PR's URL on stdout — gh's actual
// output for a created PR.
func TestGHPRCreate_UsesFlagsGHPRCreateHasAndReadsTheURLItPrints(t *testing.T) {
	fakeGHScript(t,
		"echo %* | findstr /C:\"--json\" >nul && (echo unknown flag: --json 1>&2 & exit /b 1)\r\n"+
			"echo Creating pull request for feature into main in acme/widgets 1>&2\r\n"+
			"echo https://github.com/acme/widgets/pull/42\r\n",
		"case \"$*\" in *--json*) echo 'unknown flag: --json' >&2; exit 1;; esac\n"+
			"echo 'Creating pull request for feature into main in acme/widgets' >&2\n"+
			"echo 'https://github.com/acme/widgets/pull/42'\n")
	c := NewClient(t.TempDir())

	resp, err := c.GHPRCreate(context.Background(), GHPRCreateRequest{Title: "Add retries", Body: "Why: flaky network"})
	if err != nil {
		t.Fatalf("gh.pr.create cannot create a pull request with the gh it is written for: %v", err)
	}
	if resp.URL != "https://github.com/acme/widgets/pull/42" {
		t.Errorf("the URL gh printed did not reach the model: %+v", resp)
	}
	if resp.Number != 42 {
		t.Errorf("the PR number was not read from the URL: %+v", resp)
	}
	if resp.Title != "Add retries" {
		t.Errorf("the title is missing from the answer: %+v", resp)
	}
}

// Filters on the pull request list, the way TestGHIssueList does for issues.
func TestGHPRList_PassesTheModelsFiltersToGH(t *testing.T) {
	readArgs := fakeGH(t, `[{"number":3,"title":"Retry uploads","state":"MERGED","author":{"login":"ann"},"url":"https://example.invalid/3","baseRefName":"main","headRefName":"retry","updatedAt":"2026-01-01T00:00:00Z"}]`, 0)
	c := NewClient(t.TempDir())

	resp, err := c.GHPRList(context.Background(), GHPRListRequest{State: "merged", Limit: 5, Base: "main"})
	if err != nil {
		t.Fatalf("GHPRList: %v", err)
	}
	args := readArgs()
	for _, pair := range [][2]string{{"--state", "merged"}, {"--limit", "5"}, {"--base", "main"}} {
		if !hasArgPair(args, pair[0], pair[1]) {
			t.Errorf("%s %s did not reach gh: %v", pair[0], pair[1], args)
		}
	}
	if len(resp.PRs) != 1 || resp.PRs[0].Author != "ann" || resp.PRs[0].Head != "retry" {
		t.Errorf("the answer was not flattened into the documented shape: %+v", resp)
	}
}
