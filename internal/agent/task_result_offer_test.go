package agent

import (
	"strings"
	"testing"
)

// general mode is a top-level mode a user may start, and its tool set ships
// task_result — the way a CHILD reports back. A main agent's task_result is
// rejected on purpose (IsChild, H11), so the schema was offering a completion
// the runtime refuses.
//
// The model does what the schema says. From the first eval run of general
// mode, with the work already correct on disk:
//
//	fs.delete scratch.go   -> ok, the file is gone
//	ls                     -> confirms
//	task_result            -> tool_failed
//	(validation_error)
//	(validation_error)     -> turn over, task marked failed
//
// It finished the chore and then could not say so.
func TestToolDefs_ATopLevelRunIsNotOfferedTaskResult(t *testing.T) {
	for _, mode := range []Mode{ModeGeneral, ModeBuild, ModeExplore} {
		a := &Agent{}
		a.opts.Mode = mode
		a.opts.IsChild = false

		for _, def := range a.computeToolDefs() {
			if def.Function.Name == "task_result" {
				t.Errorf("%s: a main agent is offered task_result and then refused when it "+
					"calls it, which leaves it no way to finish", mode)
			}
		}
	}
}

// A child still needs it: that is how it reports at all.
func TestToolDefs_AChildKeepsTaskResult(t *testing.T) {
	a := &Agent{}
	a.opts.Mode = ModeGeneral
	a.opts.IsChild = true

	found := false
	for _, def := range a.computeToolDefs() {
		if def.Function.Name == "task_result" {
			found = true
		}
	}
	if !found {
		t.Error("a spawned agent must keep task_result — it is its only way to report back")
	}
}

// The tool list is what the model reads; the guard that refuses the call is a
// second line, not the first. Both stay.
func TestTaskResultRefusal_StillNamesTheWayOut(t *testing.T) {
	msg := duplicateCallRefusal("write")
	if !strings.Contains(msg, `{"patches":[]}`) {
		t.Errorf("unrelated guard changed shape: %s", msg)
	}
}
