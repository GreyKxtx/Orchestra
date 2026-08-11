package tasks_test

import (
	"testing"

	"github.com/orchestra/orchestra/internal/tasks"
)

func TestParseWorkOrderJSON_Valid(t *testing.T) {
	raw := `{"intent":"fix nil deref","target_file":"a.go","target_symbol":"Foo","acceptance_criteria":["tests pass"]}`
	wo, err := tasks.ParseWorkOrderJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if wo.TargetSymbol != "Foo" {
		t.Fatalf("%+v", wo)
	}
}

func TestParseWorkOrderJSON_MissingIntent(t *testing.T) {
	_, err := tasks.ParseWorkOrderJSON(`{"target_file":"a.go"}`)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseWorkOrderJSON_NotJSON(t *testing.T) {
	_, err := tasks.ParseWorkOrderJSON("just text")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEditScopePaths(t *testing.T) {
	wo := &tasks.WorkOrder{TargetFile: "a.go"}
	if got := tasks.EditScopePaths(wo); len(got) != 1 || got[0] != "a.go" {
		t.Fatalf("target_file: %v", got)
	}
	wo2 := &tasks.WorkOrder{
		TargetFile:  "ignored.go",
		TargetFiles: []string{"x.go", "y.go"},
	}
	got := tasks.EditScopePaths(wo2)
	if len(got) != 2 || got[0] != "x.go" || got[1] != "y.go" {
		t.Fatalf("target_files wins: %v", got)
	}
	if tasks.EditScopePaths(nil) != nil {
		t.Fatal("nil wo")
	}
}
