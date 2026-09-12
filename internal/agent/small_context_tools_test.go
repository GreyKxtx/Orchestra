package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

// On a 25k local model the tool schemas are most of what the model is charged
// before it reads a single file: build mode spends 2838 bytes on the system
// prompt and 15939 on schemas. Parameter descriptions are ~2.5 KB of that,
// and they largely restate what the schema's own type, enum and minimum
// already say — so on a small window they are the first thing to go.
//
// Tool descriptions are NOT dropped: choosing the wrong tool is the mistake
// small models make most, and that text is what prevents it.
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

func TestSmallContext_DropsParameterDescriptionsButKeepsToolDescriptions(t *testing.T) {
	big := &Agent{opts: Options{Mode: ModeBuild, ModelContextTokens: 200000}}
	small := &Agent{opts: Options{Mode: ModeBuild, ModelContextTokens: 25000}}

	bigBytes, bigParamDesc, bigToolDesc := toolSchemaBytes(t, big)
	smallBytes, smallParamDesc, smallToolDesc := toolSchemaBytes(t, small)

	if !bigParamDesc {
		t.Fatal("fixture invalid: the full schemas must carry parameter descriptions")
	}
	if smallParamDesc {
		t.Error("on a small window the parameter descriptions must be gone")
	}
	if !bigToolDesc || !smallToolDesc {
		t.Error("tool descriptions must survive at every window size — they are what stops the wrong tool being called")
	}
	if smallBytes >= bigBytes {
		t.Errorf("the small-window schemas must be smaller: %d vs %d", smallBytes, bigBytes)
	}
	if saved := bigBytes - smallBytes; saved < 1500 {
		t.Errorf("only %d bytes saved — not worth a behaviour difference", saved)
	}
}

// A model whose window is unknown (0) is not assumed to be small.
func TestSmallContext_UnknownWindowKeepsTheFullSchemas(t *testing.T) {
	unknown := &Agent{opts: Options{Mode: ModeBuild}}
	_, hasParamDesc, _ := toolSchemaBytes(t, unknown)
	if !hasParamDesc {
		t.Error("with no declared window the full schemas must be sent")
	}
}

// The compaction must not disturb the schemas themselves: a model still has
// to know a parameter's type, its enum values and whether it is required.
func TestSmallContext_KeepsTheSchemaStructure(t *testing.T) {
	small := &Agent{opts: Options{Mode: ModeBuild, ModelContextTokens: 25000}}
	for _, d := range small.computeToolDefs() {
		if d.Function.Name != "memory_write" {
			continue
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
