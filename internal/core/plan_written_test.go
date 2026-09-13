package core

import (
	"os"
	"path/filepath"
	"testing"
)

// A run reports where its plan is. It used to report where a plan WOULD go,
// which is a different fact and is true of every plan-mode run whether or not
// the model ever planned.
//
// The eval task plan_locates_the_fix failed three runs in five with
//
//	file_exists "{plan}": cannot find the path specified
//	   …\.orchestra\plans\20260913-143303-plan.md
//
// and the kept workspaces had no plans directory at all: the model explored,
// never called plan_exit, and the run handed back a path anyway. The failure
// read as a missing file; the truth was that nothing was ever written. A UI
// offering to open that plan would have opened nothing.

func TestWrittenPlanPath_APlanThatWasWrittenIsReported(t *testing.T) {
	root := t.TempDir()
	rel := ".orchestra/plans/20260913-143303-plan.md"
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("# Plan\n\nFix the nil map in internal/store.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := writtenPlanPath(root, rel); got != rel {
		t.Errorf("a plan that exists must be reported: got %q, want %q", got, rel)
	}
}

func TestWrittenPlanPath_APlanThatWasNeverWrittenIsNotReported(t *testing.T) {
	root := t.TempDir()
	rel := ".orchestra/plans/20260913-143303-plan.md"

	if got := writtenPlanPath(root, rel); got != "" {
		t.Errorf("the run wrote no plan, so it must report none; got %q", got)
	}
}

// An empty file is the shape a half-finished write leaves behind, and it is
// not a plan. Reporting it sends the caller to a blank page and calls that
// success.
func TestWrittenPlanPath_AnEmptyPlanFileIsNotAPlan(t *testing.T) {
	root := t.TempDir()
	rel := ".orchestra/plans/empty-plan.md"
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if got := writtenPlanPath(root, rel); got != "" {
		t.Errorf("an empty file is not a plan; got %q", got)
	}
}

// Modes that never plan configure no path, and must keep reporting none.
func TestWrittenPlanPath_NoConfiguredPathStaysEmpty(t *testing.T) {
	if got := writtenPlanPath(t.TempDir(), ""); got != "" {
		t.Errorf("no configured plan path must stay empty; got %q", got)
	}
}
