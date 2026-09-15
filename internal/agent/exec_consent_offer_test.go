package agent

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/protocol/schema"
)

// A TUI or IDE turn with "shell · ask" has no static exec consent but a
// requester that asks the user before each command. bash was never offered to
// such a turn, so the ask never came: seen in the TUI with the 27B, the model
// said "this environment has no shell tool", spawned a child agent to find one
// and ended the turn unable to run go test. The turn is offered bash, and asks.
//
// git.commit, git.push and the rest do not ask before they run, so a consent
// that exists only per call does not unlock them.
func TestAgent_ARequesterUnlocksBashButNotTheRepoMutatingTools(t *testing.T) {
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tools.NewRunner(t.TempDir(), tools.RunnerOptions{BlockExecInDryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	offered := func(opts Options) map[string]bool {
		ag, err := New(&questionLLM{}, v, tr, opts)
		if err != nil {
			t.Fatal(err)
		}
		names := map[string]bool{}
		for _, d := range ag.buildToolDefs() {
			names[d.Function.Name] = true
		}
		return names
	}

	asks := offered(Options{Mode: ModeBuild, PermissionRequester: &scriptedRequester{approve: true}})
	for _, name := range []string{"bash", "bash.output", "bash.kill"} {
		if !asks[name] {
			t.Errorf("a turn that can ask the user is not offered %s", name)
		}
	}
	for _, name := range []string{"git.commit", "git.branch", "git.checkout", "git.push", "git.worktree.add", "gh.pr.create"} {
		if asks[name] {
			t.Errorf("a turn with per-call consent only is offered %s, which runs without asking", name)
		}
	}

	if got := offered(Options{Mode: ModeBuild}); got["bash"] {
		t.Error("a turn with no consent and no requester is offered bash")
	}
	if got := offered(Options{Mode: ModeBuild, AllowExec: true}); !got["bash"] || !got["git.commit"] {
		t.Error("static exec consent must still offer bash and git.commit")
	}

	tr.SetDryRun(true)
	if got := offered(Options{Mode: ModeBuild, PermissionRequester: &scriptedRequester{approve: true}}); got["bash"] {
		t.Error("a preview turn refuses every command; a requester does not change that")
	}
}

// The offered bash asks, and the answer decides: a yes runs the command, a no
// refuses it.
func TestAgent_BashOfferedByARequesterRunsOnlyOnAYes(t *testing.T) {
	yes := &scriptedRequester{approve: true}
	got := runCalls(t, Options{Mode: ModeBuild, PermissionRequester: yes},
		toolCall("b1", "bash", `{"command":"echo consent-given"}`))["b1"]
	if len(yes.asked) != 1 || yes.asked[0].Tool != "bash" {
		t.Fatalf("the user was not asked before the command: %+v", yes.asked)
	}
	if !strings.Contains(got, "consent-given") {
		t.Errorf("an approved command did not run: %s", got)
	}

	no := &scriptedRequester{approve: false}
	got = runCalls(t, Options{Mode: ModeBuild, PermissionRequester: no},
		toolCall("b1", "bash", `{"command":"echo consent-given"}`))["b1"]
	if len(no.asked) != 1 || !strings.Contains(got, `"status":"denied"`) {
		t.Errorf("a refused command ran or was not asked about (asked %d): %s", len(no.asked), got)
	}
}
