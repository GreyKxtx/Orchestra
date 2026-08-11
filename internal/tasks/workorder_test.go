package tasks

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFormatChildGoal_WorkerWrapsPlainText(t *testing.T) {
	got := FormatChildGoal("worker", "micro", "rename Foo to Bar in x.go")
	var m map[string]any
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Fatalf("expected JSON WorkOrder: %v\n%s", err, got)
	}
	if m["tier"] != "micro" {
		t.Fatalf("tier=%v", m["tier"])
	}
	ac, ok := m["acceptance_criteria"].([]any)
	if !ok || len(ac) == 0 {
		t.Fatalf("missing acceptance_criteria: %#v", m["acceptance_criteria"])
	}
}

func TestFormatChildGoal_WorkerKeepsJSON(t *testing.T) {
	in := `{"intent":"fix","target_file":"a.go","acceptance_criteria":["done"]}`
	got := FormatChildGoal("worker", "focused", in)
	if !strings.Contains(got, `"intent"`) {
		t.Fatalf("unexpected: %s", got)
	}
}

func TestParseWorkOrderJSON_CKGFields(t *testing.T) {
	raw := `{
		"intent": "fix handler",
		"target_files": ["internal/api/handler.go"],
		"readonly_references": ["internal/core/models.go"],
		"allowed_symbols": ["auth.ValidateToken"]
	}`
	wo, err := ParseWorkOrderJSON(raw)
	if err != nil {
		t.Fatalf("ParseWorkOrderJSON: %v", err)
	}
	if len(wo.TargetFiles) != 1 || wo.TargetFiles[0] != "internal/api/handler.go" {
		t.Fatalf("target_files: %#v", wo.TargetFiles)
	}
	if len(wo.ReadonlyReferences) != 1 {
		t.Fatalf("readonly_references: %#v", wo.ReadonlyReferences)
	}
	if len(wo.AllowedSymbols) != 1 || wo.AllowedSymbols[0] != "auth.ValidateToken" {
		t.Fatalf("allowed_symbols: %#v", wo.AllowedSymbols)
	}
}

func TestFormatChildGoal_ExplorePassthrough(t *testing.T) {
	got := FormatChildGoal("explore", "", "find Foo usages")
	if got != "find Foo usages" {
		t.Fatalf("got %q", got)
	}
}
