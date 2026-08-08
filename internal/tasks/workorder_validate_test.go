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
