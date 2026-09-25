package cli

import (
	"strings"
	"testing"

	evalharness "github.com/orchestra/orchestra/internal/eval"
)

// --task exists so that one task can be iterated on without paying for the
// whole suite. That makes its failure mode expensive: a run that matches
// nothing prints the same "0 failed" as a run that matched everything and
// passed, and the mistake is invisible at exactly the moment someone is
// changing a task and watching for a change in the result.

func namedTasks(names ...string) []evalharness.Task {
	tasks := make([]evalharness.Task, 0, len(names))
	for _, n := range names {
		tasks = append(tasks, evalharness.Task{Name: n})
	}
	return tasks
}

// The reason --repeat exists. A single run cannot distinguish "this works"
// from "this worked once", and the suite spent a day reporting the second as
// the first — refactor failed a run and passed the next with nothing changed
// between them, both reported as plain verdicts.
func TestEvalStatus_SomeWinsAndSomeLossesIsNeitherPassNorFail(t *testing.T) {
	for _, c := range []struct {
		won, runs int
		want      string
	}{
		{1, 1, "PASS"},
		{0, 1, "FAIL"},
		{3, 3, "PASS"},
		{0, 3, "FAIL"},
		{2, 3, "FLAKY"},
		{1, 3, "FLAKY"},
		{1, 2, "FLAKY"},
		{0, 0, "FAIL"}, // nothing ran, so nothing was established
	} {
		if got := evalStatus(c.won, c.runs); got != c.want {
			t.Errorf("evalStatus(%d, %d) = %q, want %q", c.won, c.runs, got, c.want)
		}
	}
}

func TestSelectTasks_NoNamesRunsEverything(t *testing.T) {
	all := namedTasks("add_func", "fix_bug", "refactor")
	got, err := selectTasks(all, nil)
	if err != nil {
		t.Fatalf("selecting nothing in particular must run the suite: %v", err)
	}
	if len(got) != len(all) {
		t.Errorf("got %d tasks, want all %d", len(got), len(all))
	}
}

func TestSelectTasks_PicksTheNamedOnesOnly(t *testing.T) {
	got, err := selectTasks(namedTasks("add_func", "fix_bug", "refactor"), []string{"refactor", "add_func"})
	if err != nil {
		t.Fatalf("selectTasks: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d tasks, want 2", len(got))
	}
	// Directory order, not the order they were asked for: the report reads the
	// same whichever way the flag was typed.
	if got[0].Name != "add_func" || got[1].Name != "refactor" {
		t.Errorf("got %q and %q", got[0].Name, got[1].Name)
	}
}

// The whole reason this returns an error.
func TestSelectTasks_ATypoIsAnErrorNotAnEmptyRun(t *testing.T) {
	got, err := selectTasks(namedTasks("add_func", "refactor"), []string{"refactr"})
	if err == nil {
		t.Fatalf("a name matching nothing must fail; got %d tasks and no error", len(got))
	}
	if !strings.Contains(err.Error(), "refactr") {
		t.Errorf("the error must name what was not found: %v", err)
	}
	if !strings.Contains(err.Error(), "add_func") || !strings.Contains(err.Error(), "refactor") {
		t.Errorf("the error must list what there is to choose from: %v", err)
	}
}

// One good name does not excuse a bad one — otherwise a typo in a two-task
// selection quietly runs half of what was asked for.
func TestSelectTasks_OneGoodNameDoesNotExcuseABadOne(t *testing.T) {
	if _, err := selectTasks(namedTasks("add_func", "refactor"), []string{"refactor", "no_such"}); err == nil {
		t.Error("a selection with one unknown name must fail")
	}
}

func TestSelectTasks_IgnoresBlankNames(t *testing.T) {
	// StringSliceVar splits on commas, so "a,,b" and a stray space are the
	// user's spelling, not a request for a task called "".
	got, err := selectTasks(namedTasks("add_func", "refactor"), []string{" refactor ", ""})
	if err != nil {
		t.Fatalf("selectTasks: %v", err)
	}
	if len(got) != 1 || got[0].Name != "refactor" {
		t.Errorf("got %v, want just refactor", got)
	}
}
