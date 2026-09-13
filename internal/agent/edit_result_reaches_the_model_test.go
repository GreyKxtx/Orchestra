package agent

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/patch/cache"
)

// The tools can describe a change perfectly and the model still never see it:
// between the tool's response and the model's history sit the result
// serialisation and prepareToolHistoryContent, either of which can drop a
// field. Nothing else in this repo tests that stretch, and it is exactly where
// --keep-failed shipped useless — the harness tests passed while the line that
// reaches the screen was never written.
//
// So this reads the tool message the agent actually fed back.

func TestEditResult_TheModelIsToldItsChangeLanded(t *testing.T) {
	_, client := runTurn(t, []string{readStep, editStep, `{"patches":[]}`})

	msgs := recordedToolMessages(client)
	if !strings.Contains(msgs, "math.go written") {
		t.Errorf("the edit's answer does not say the change was made, so the model's only "+
			"news of its own edit is a hash:\n%s", msgs)
	}
	if !strings.Contains(msgs, "func Multiply") {
		t.Errorf("the model cannot see the code it just added:\n%s", msgs)
	}
	// Numbered, because an unplaced snippet does not tell the model where in
	// the file its change ended up.
	if !strings.Contains(msgs, "9: func Multiply(a, b int) int {") {
		t.Errorf("the changed region must carry line numbers of the new file:\n%s", msgs)
	}
}

// The other half, and the one that decides whether the model can trust the
// report: writing a file the content it already has must not come back
// claiming something was written. A model that re-sends what is already there
// is a model going in circles, and "written" tells it to keep going.
//
// An identical second edit cannot be used to test this — the duplicate-call
// blocker refuses it first, which is its job. A write of unchanged content is
// the shape that actually reaches the tool.
func TestEditResult_AWriteOfUnchangedContentDoesNotClaimItChangedSomething(t *testing.T) {
	_, client := runTurn(t, []string{
		readStep,
		editStep,
		`{"type":"tool_call","tool":{"name":"write","input":{"path":"math.go",` +
			`"content":` + jsonString(mathAfterEdit) + `,` +
			`"file_hash":` + jsonString(cache.ComputeSHA256([]byte(mathAfterEdit))) + `}}}`,
		`{"patches":[]}`,
	})

	msgs := recordedToolMessages(client)
	if !strings.Contains(msgs, "unchanged") {
		t.Errorf("a write that changed nothing must say so, or a model that is looping "+
			"believes it is making progress:\n%s", msgs)
	}
}
