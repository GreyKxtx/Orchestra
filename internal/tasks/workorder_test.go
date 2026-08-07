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

func TestFormatChildGoal_ExplorePassthrough(t *testing.T) {
	got := FormatChildGoal("explore", "", "find Foo usages")
	if got != "find Foo usages" {
		t.Fatalf("got %q", got)
	}
}
