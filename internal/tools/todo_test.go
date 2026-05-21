package tools

import "testing"

func TestValidateTodos(t *testing.T) {
	got, err := ValidateTodos([]TodoItem{
		{ID: "1", Content: "a", Status: "completed"},
		{ID: "2", Content: "b", Status: TodoPending},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Status != TodoDone {
		t.Fatalf("expected completed→done, got %q", got[0].Status)
	}
}

func TestValidateTodos_TwoInProgress(t *testing.T) {
	_, err := ValidateTodos([]TodoItem{
		{ID: "1", Content: "a", Status: TodoInProgress},
		{ID: "2", Content: "b", Status: TodoInProgress},
	})
	if err == nil {
		t.Fatal("expected error for two in_progress")
	}
}
