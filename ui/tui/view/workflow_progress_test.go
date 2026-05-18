package view

import (
	"strings"
	"testing"
)

// TestWorkflowProgress_Lifecycle drives the widget through a typical
// workflow: begin → two stages run-then-done → render shows expected glyphs
// in the expected order → end clears.
func TestWorkflowProgress_Lifecycle(t *testing.T) {
	w := NewWorkflowProgress(80)
	if w.Active() {
		t.Fatalf("widget should start inactive")
	}
	if w.Render() != "" {
		t.Fatalf("inactive widget must render empty")
	}

	w.Begin("tdd_feature")
	if !w.Active() {
		t.Fatalf("widget should be active after Begin")
	}
	w.StageStart("spec")
	w.StageDone("spec", "advance")
	w.StageStart("tests")

	out := stripStyling(w.Render())
	if !strings.Contains(out, "workflow:tdd_feature") {
		t.Errorf("header missing: %q", out)
	}
	if !strings.Contains(out, "✓ spec") {
		t.Errorf("done glyph missing for spec: %q", out)
	}
	if !strings.Contains(out, "⋯ tests") {
		t.Errorf("running glyph missing for tests: %q", out)
	}

	// Redo on tests resets it to redo glyph; subsequent StageStart flips it
	// back to running. Stage order preserved.
	w.StageDone("tests", "redo:spec")
	if got := stripStyling(w.Render()); !strings.Contains(got, "↻ tests") {
		t.Errorf("redo glyph missing: %q", got)
	}
	w.StageStart("tests")
	if got := stripStyling(w.Render()); !strings.Contains(got, "⋯ tests") {
		t.Errorf("re-run did not flip back to running: %q", got)
	}

	w.End()
	if w.Active() || w.Render() != "" {
		t.Fatalf("End() should clear state")
	}
}

// TestWorkflowProgress_Failure routes action=fail to the ✗ glyph.
func TestWorkflowProgress_Failure(t *testing.T) {
	w := NewWorkflowProgress(80)
	w.Begin("x")
	w.StageStart("a")
	w.StageDone("a", "fail")
	if got := stripStyling(w.Render()); !strings.Contains(got, "✗ a") {
		t.Errorf("fail glyph missing: %q", got)
	}
}

// TestWorkflowProgress_StageOrder ensures stages appear in the order the
// runner emitted them (insertion order), not alphabetical.
func TestWorkflowProgress_StageOrder(t *testing.T) {
	w := NewWorkflowProgress(120)
	w.Begin("o")
	for _, id := range []string{"zeta", "alpha", "mu"} {
		w.StageStart(id)
		w.StageDone(id, "advance")
	}
	out := stripStyling(w.Render())
	zi := strings.Index(out, "zeta")
	ai := strings.Index(out, "alpha")
	mi := strings.Index(out, "mu")
	if zi < 0 || ai < 0 || mi < 0 {
		t.Fatalf("not all stages rendered: %q", out)
	}
	if !(zi < ai && ai < mi) {
		t.Errorf("stage order broken: zeta@%d alpha@%d mu@%d in %q", zi, ai, mi, out)
	}
}
