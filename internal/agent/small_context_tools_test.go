package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
)

// These tests used to assert the opposite, and the reasoning behind them was
// sound: on a 25k local model the tool schemas are most of what the model is
// charged before it reads a single file — build mode spends 2838 bytes on the
// system prompt and 15939 on schemas — and the ~2.5 KB of parameter prose
// largely restates what the schema's own type, enum and minimum already say.
//
// The suite disagreed. Three rounds of twelve tasks against a 9B model at a
// 25k window: 24/36 with the full schemas, 20/36 without the descriptions.
// The loss was concentrated, not spread — edit-in-large-file was the one
// heavy task that passed all three rounds and then failed all three, every
// time with the model re-searching for text it had already replaced.
//
// So the schemas are sent whole at every window size, and the tests below
// hold that. tools.CompactSchemasForSmallContext is kept and tested on its
// own: the decision came from a measurement, and a different model or a
// tighter window could reverse it.

func toolSchemaBytes(t *testing.T, a *Agent) (total int, hasParamDesc bool, hasToolDesc bool) {
	t.Helper()
	for _, d := range a.computeToolDefs() {
		b, err := json.Marshal(d)
		if err != nil {
			t.Fatalf("marshal %s: %v", d.Function.Name, err)
		}
		total += len(b)
		if strings.Contains(string(d.Function.Parameters), `"description"`) {
			hasParamDesc = true
		}
		if strings.TrimSpace(d.Function.Description) != "" {
			hasToolDesc = true
		}
	}
	return total, hasParamDesc, hasToolDesc
}

func TestSmallContext_SendsTheSameSchemasAsAnyOtherWindow(t *testing.T) {
	big := &Agent{opts: Options{Mode: ModeBuild, ModelContextTokens: 200000}}
	small := &Agent{opts: Options{Mode: ModeBuild, ModelContextTokens: 25000}}

	bigBytes, bigParamDesc, bigToolDesc := toolSchemaBytes(t, big)
	smallBytes, smallParamDesc, smallToolDesc := toolSchemaBytes(t, small)

	if !bigParamDesc {
		t.Fatal("fixture invalid: the full schemas must carry parameter descriptions")
	}
	if !smallParamDesc {
		t.Error("a small window gets the parameter descriptions too — dropping them cost 4 of 36 tasks")
	}
	if !bigToolDesc || !smallToolDesc {
		t.Error("tool descriptions must survive at every window size — they are what stops the wrong tool being called")
	}
	if smallBytes != bigBytes {
		t.Errorf("the window size must not change the schemas at all: %d vs %d", smallBytes, bigBytes)
	}
}

// A model whose window is unknown (0) is treated no differently either.
func TestSmallContext_UnknownWindowKeepsTheFullSchemas(t *testing.T) {
	unknown := &Agent{opts: Options{Mode: ModeBuild}}
	_, hasParamDesc, _ := toolSchemaBytes(t, unknown)
	if !hasParamDesc {
		t.Error("with no declared window the full schemas must be sent")
	}
}

// The compaction helper is unused by the agent but still correct, and the
// part worth holding is that it never damages a schema: a model reading a
// compacted schema still has to know a parameter's type, its enum values and
// whether it is required. Anyone wiring this back in starts from here.
func TestCompactSchemas_KeepsTheSchemaStructure(t *testing.T) {
	full := (&Agent{opts: Options{Mode: ModeBuild, ModelContextTokens: 200000}}).computeToolDefs()
	compact := tools.CompactSchemasForSmallContext(full)
	if len(compact) != len(full) {
		t.Fatalf("compaction dropped tools: %d vs %d", len(compact), len(full))
	}
	for _, d := range compact {
		if d.Function.Name != "memory_write" {
			continue
		}
		if strings.Contains(string(d.Function.Parameters), `"description"`) {
			t.Error("compaction is supposed to remove the parameter prose")
		}
		var schema map[string]any
		if err := json.Unmarshal(d.Function.Parameters, &schema); err != nil {
			t.Fatalf("compaction produced invalid JSON schema: %v", err)
		}
		props, _ := schema["properties"].(map[string]any)
		typ, _ := props["type"].(map[string]any)
		if typ == nil {
			t.Fatal("memory_write lost its type property")
		}
		if _, ok := typ["enum"]; !ok {
			t.Error("the enum must survive — it is the part the model cannot guess")
		}
		if _, ok := schema["required"]; !ok {
			t.Error("required must survive")
		}
		return
	}
	t.Fatal("fixture invalid: build mode should offer memory_write")
}
