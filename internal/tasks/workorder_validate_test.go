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

// The Lead prompt lists acceptance_checks[] without a shape; a 27B Lead wrote
// bare command strings and two WorkOrders were refused for it.
func TestParseWorkOrderJSON_AcceptanceChecksAsStrings(t *testing.T) {
	raw := `{"intent":"build it","target_file":"a.go","acceptance_checks":["go build ./...",{"cmd":"go test ./...","expect_exit":0}]}`
	wo, err := tasks.ParseWorkOrderJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(wo.AcceptanceChecks) != 2 {
		t.Fatalf("checks: %+v", wo.AcceptanceChecks)
	}
	if wo.AcceptanceChecks[0].Cmd != "go build ./..." || wo.AcceptanceChecks[0].ExpectExit != 0 {
		t.Fatalf("string form: %+v", wo.AcceptanceChecks[0])
	}
	if wo.AcceptanceChecks[1].Cmd != "go test ./..." {
		t.Fatalf("object form: %+v", wo.AcceptanceChecks[1])
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
