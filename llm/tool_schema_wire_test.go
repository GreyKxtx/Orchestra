package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSanitizeWireToolSchema_StripsTopLevelCombinators(t *testing.T) {
	in := json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"anyOf": [{"required":["prompt"]},{"required":["goal"]}],
		"properties": {"prompt":{"type":"string"},"goal":{"type":"string"}}
	}`)
	out, changed := sanitizeWireToolSchema(in)
	if !changed {
		t.Fatal("expected change")
	}
	s := string(out)
	if strings.Contains(s, "anyOf") {
		t.Fatalf("anyOf survived: %s", s)
	}
	if !strings.Contains(s, `"properties"`) || !strings.Contains(s, `"prompt"`) {
		t.Fatalf("properties lost: %s", s)
	}
}

func TestSanitizeWireToolSchema_KeepsNestedCombinators(t *testing.T) {
	in := json.RawMessage(`{
		"type": "object",
		"properties": {"x":{"anyOf":[{"type":"string"},{"type":"number"}]}}
	}`)
	out, changed := sanitizeWireToolSchema(in)
	if changed {
		t.Fatalf("nested anyOf must be kept, got change: %s", out)
	}
}

func TestSanitizeWireToolSchema_AddsTypeWhenOnlyCombinator(t *testing.T) {
	in := json.RawMessage(`{"oneOf":[{"type":"object"},{"type":"string"}]}`)
	out, changed := sanitizeWireToolSchema(in)
	if !changed {
		t.Fatal("expected change")
	}
	if !strings.Contains(string(out), `"type":"object"`) {
		t.Fatalf("missing default type: %s", out)
	}
}

func TestSanitizeWireToolSchemas_LazyCopy(t *testing.T) {
	clean := []ToolDef{{Type: "function", Function: ToolFunctionDef{
		Name: "read", Parameters: json.RawMessage(`{"type":"object"}`),
	}}}
	if out := sanitizeWireToolSchemas(clean); &out[0] != &clean[0] {
		t.Fatal("expected same slice when nothing changes")
	}

	dirty := []ToolDef{
		{Type: "function", Function: ToolFunctionDef{Name: "read", Parameters: json.RawMessage(`{"type":"object"}`)}},
		{Type: "function", Function: ToolFunctionDef{Name: "task", Parameters: json.RawMessage(`{"type":"object","anyOf":[{"required":["a"]}]}`)}},
	}
	out := sanitizeWireToolSchemas(dirty)
	if strings.Contains(string(out[1].Function.Parameters), "anyOf") {
		t.Fatalf("anyOf survived: %s", out[1].Function.Parameters)
	}
	// Canonical defs must stay untouched (runtime validation relies on them).
	if !strings.Contains(string(dirty[1].Function.Parameters), "anyOf") {
		t.Fatal("input slice mutated")
	}
}
