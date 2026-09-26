package agent

import (
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/protocol/schema"
)

// core refuses every command in a preview turn (apply off), yet the turn was
// still offered bash, bash.output and bash.kill. Seen in the eval: the model
// called bash eight times, got "exec.run disabled in dry-run preview … Shift+Tab"
// each time, and the turn died on the breaker. A tool the runtime will refuse
// for the whole turn is not offered; once apply unlocks commands, it is.
func TestAgent_OffersNoCommandsToATurnThatWillRefuseThem(t *testing.T) {
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tools.NewRunner(t.TempDir(), tools.RunnerOptions{BlockExecInDryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	tr.SetDryRun(true)

	offered := func() map[string]bool {
		ag, err := New(&questionLLM{}, v, tr, Options{Mode: ModeBuild, AllowExec: true})
		if err != nil {
			t.Fatal(err)
		}
		names := map[string]bool{}
		for _, d := range ag.buildToolDefs() {
			names[d.Function.Name] = true
		}
		return names
	}

	got := offered()
	for _, name := range []string{"bash", "bash.output", "bash.kill"} {
		if got[name] {
			t.Errorf("a preview turn is offered %s, which the runner refuses for the whole turn", name)
		}
	}

	tr.SetAllowExecDespiteDryRun(true)
	if !offered()["bash"] {
		t.Error("with apply unlocking commands the turn must be offered bash")
	}

	// A preview whose commands run in a shadow of the workspace (exec.shadow)
	// refuses nothing, so it is offered bash.
	shadowed, err := tools.NewRunner(t.TempDir(), tools.RunnerOptions{BlockExecInDryRun: true, ShadowExec: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shadowed.Close() })
	shadowed.SetDryRun(true)
	ag, err := New(&questionLLM{}, v, shadowed, Options{Mode: ModeBuild, AllowExec: true})
	if err != nil {
		t.Fatal(err)
	}
	got = map[string]bool{}
	for _, d := range ag.buildToolDefs() {
		got[d.Function.Name] = true
	}
	if !got["bash"] {
		t.Error("a preview with a shadow workspace must be offered bash: its commands run there")
	}
}
