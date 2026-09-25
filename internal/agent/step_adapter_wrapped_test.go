package agent

import (
	"testing"

	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// LLM-13: a wrapped final the schema refused — here for a patch without its
// file_hash — was read as a bare PatchSet, found no top-level "patches", and
// became a final with none: the model's edit was dropped without a word.
func TestNormalize_AWrappedFinalKeepsItsPatches(t *testing.T) {
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	resp := &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[` +
		`{"type":"file.search_replace","path":"a.go","search":"var x = 1","replace":"var x = 2"}]}}`}}
	step, _, err := NormalizeLLM(v, resp)
	if err != nil {
		t.Fatal(err)
	}
	if step.Final == nil || len(step.Final.Patches) != 1 || step.Final.Patches[0].Path != "a.go" {
		t.Fatalf("the wrapped patch was dropped: %+v", step.Final)
	}
}
