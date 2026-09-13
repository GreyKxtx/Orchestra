package agent

import (
	"testing"
)

// A turn that finished its work with a delete could not say it had finished.
//
// rejectPrematureFinal lets a final through when the turn already mutated
// something — a.turnMutatingTools — and that counter was incremented for
// write and edit only. fs.delete and fs.rename change the workspace just as
// much, so a turn whose whole job was a deletion looked, at the final step,
// exactly like a turn that had done nothing, and was told to "call read, then
// edit or write" for a file it had correctly removed.
//
// Both eval runs of general mode ended this way, with the work already right
// on disk:
//
//	fs.delete scratch.go  -> ok, the file is gone
//	(validation_error)
//	(validation_error)    -> "model repeatedly produced invalid output"
//
// The same happened in build mode to a turn whose work was a memory_write, so
// this was never about modes.
func TestPrematureFinal_ATurnThatDeletedAFileMayFinish(t *testing.T) {
	for _, tool := range []string{"fs.delete", "fs.rename", "write", "edit"} {
		a := &Agent{}
		a.countMutatingTool(tool)

		hint, reject := a.rejectPrematureFinal(
			"delete scratch.go, it is dead code",
			&Step{Type: StepFinal, Final: &Final{}},
			`{"patches":[]}`,
			4,
		)
		if reject {
			t.Errorf("after %s the turn was refused its final:\n%s", tool, hint)
		}
	}
}

// The guard still has to do its job: a turn that changed nothing and was asked
// to change something must not be allowed to claim it is done.
func TestPrematureFinal_ATurnThatChangedNothingIsStillRefused(t *testing.T) {
	a := &Agent{}

	_, reject := a.rejectPrematureFinal(
		"fix the compile error in broken.go",
		&Step{Type: StepFinal, Final: &Final{}},
		`{"patches":[]}`,
		4,
	)
	if !reject {
		t.Error("a turn that called no mutating tool must not final on a query that asks for a change")
	}
}

// And a read-only tool is not work: counting ls would open the same hole from
// the other side.
func TestPrematureFinal_ReadOnlyCallsAreNotMutations(t *testing.T) {
	a := &Agent{}
	a.countMutatingTool("ls")
	a.countMutatingTool("read")

	if a.turnMutatingTools != 0 {
		t.Errorf("read-only calls counted as mutations: %d", a.turnMutatingTools)
	}
}
