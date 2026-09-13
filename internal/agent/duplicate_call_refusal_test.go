package agent

import (
	"strings"
	"testing"
)

// From a real fix_compile_error run (orch-eval-526045348). The model wrote
// go.mod with content the file already had, was told the write changed
// nothing, and came back to it:
//
//	write go.mod  -> disk_commit "commit changed no file"
//	write main.go -> 121 bytes committed
//	write go.mod  -> denied: duplicate call with identical arguments
//	read  go.mod
//	write go.mod  -> denied
//	write main.go -> denied, turn over
//
// The refusal it received each time ended with "Use edit/write to apply
// changes or call with different arguments" — advice to do the thing it had
// just been refused for. A refusal that names the wrong way out is worse than
// a bare error: it reads as instruction.
func TestDuplicateCallRefusal_ARepeatedWriteIsNotToldToWriteAgain(t *testing.T) {
	for _, name := range []string{"write", "edit"} {
		msg := duplicateCallRefusal(name)

		if strings.Contains(msg, "Use edit/write") {
			t.Errorf("«%s» was refused and told to use edit/write — that is the call "+
				"that was just refused:\n%s", name, msg)
		}
		if !strings.Contains(msg, "already") {
			t.Errorf("the refusal must say the file already holds this content, or the "+
				"model cannot tell a rejected change from an applied one:\n%s", msg)
		}
		if !strings.Contains(msg, `{"patches":[]}`) {
			t.Errorf("the refusal must name the way out of the turn, or the model has "+
				"nowhere to go but back to the same call:\n%s", msg)
		}
	}
}

// The old advice is right for every tool that is not itself an edit: a
// repeated bash or fs.rename really should give way to a write.
func TestDuplicateCallRefusal_AnyOtherToolIsStillPointedAtEditing(t *testing.T) {
	msg := duplicateCallRefusal("bash")

	if !strings.Contains(msg, "edit/write") {
		t.Errorf("a repeated non-editing call should still be pointed at making the "+
			"change:\n%s", msg)
	}
	if !strings.Contains(msg, "bash") {
		t.Errorf("the refusal must name the tool it is about:\n%s", msg)
	}
}
