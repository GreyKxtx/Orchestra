package tools

import (
	"encoding/json"
	"testing"
)

func TestToolRegistry_AllowExecFalse_NoExecRun(t *testing.T) {
	defs := ListTools(false)
	for _, d := range defs {
		if d.Function.Name == "bash" {
			t.Fatalf("bash must not be exposed when allowExec=false (got %q)", d.Function.Name)
		}
	}
}

func TestToolRegistry_SchemasAreValidJSON(t *testing.T) {
	defs := ListTools(true)
	for _, d := range defs {
		if d.Type != "function" {
			t.Fatalf("unexpected tool type %q for %s", d.Type, d.Function.Name)
		}
		if d.Function.Name == "" {
			t.Fatalf("tool name is empty")
		}
		if len(d.Function.Parameters) == 0 {
			t.Fatalf("missing parameters schema for %s", d.Function.Name)
		}
		var v map[string]json.RawMessage
		if err := json.Unmarshal(d.Function.Parameters, &v); err != nil {
			t.Fatalf("invalid JSON schema for %s: %v", d.Function.Name, err)
		}
		if _, ok := v["type"]; !ok {
			t.Fatalf("schema for %s must have top-level 'type'", d.Function.Name)
		}
	}
}
