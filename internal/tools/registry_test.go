package tools

import (
	"encoding/json"
	"testing"
)

func TestToolRegistry_AllowExecFalse_NoExecRun(t *testing.T) {
	defs := ListTools(false, false)
	for _, d := range defs {
		if d.Function.Name == "bash" {
			t.Fatalf("bash must not be exposed when allowExec=false (got %q)", d.Function.Name)
		}
	}
}

func TestResolveToolNames(t *testing.T) {
	cases := []struct {
		name    string
		input   []string
		wantLen int
		wantErr bool
	}{
		{"single known", []string{"read"}, 1, false},
		{"multiple known", []string{"read", "grep", "write"}, 3, false},
		{"all tools", []string{"ls", "read", "glob", "write", "edit", "grep", "symbols",
			"explore", "bash", "webfetch", "todowrite", "todoread", "memory_write",
			"runtime_query", "task_spawn", "task_wait", "task_cancel", "task_result",
			"plan_enter", "plan_exit", "question"}, 21, false},
		{"unknown tool", []string{"read", "fly"}, 0, true},
		{"empty list", []string{}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveToolNames(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != tc.wantLen {
				t.Errorf("len = %d, want %d", len(got), tc.wantLen)
			}
		})
	}
}

func TestResolveToolNames_PreservesOrder(t *testing.T) {
	names := []string{"write", "read", "grep"}
	defs, err := ResolveToolNames(names)
	if err != nil {
		t.Fatal(err)
	}
	for i, d := range defs {
		if d.Function.Name != names[i] {
			t.Errorf("position %d: got %q, want %q", i, d.Function.Name, names[i])
		}
	}
}

func TestToolRegistry_SchemasAreValidJSON(t *testing.T) {
	defs := ListTools(true, true)
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
