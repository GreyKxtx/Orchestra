package agent

import (
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
)

func TestMergeAgentResults_prefersBuildTodosWhenPresent(t *testing.T) {
	first := &Result{Todos: []tools.TodoItem{{Content: "from-plan", Status: tools.TodoPending}}}
	second := &Result{Todos: []tools.TodoItem{{Content: "from-build", Status: tools.TodoPending}}}
	got := mergeAgentResults(first, second)
	if len(got.Todos) != 1 || got.Todos[0].Content != "from-build" {
		t.Fatalf("todos=%+v", got.Todos)
	}
}

func TestMergeAgentResults_keepsPlanTodosWhenBuildEmpty(t *testing.T) {
	first := &Result{Todos: []tools.TodoItem{{Content: "from-plan", Status: tools.TodoPending}}}
	second := &Result{Todos: nil}
	got := mergeAgentResults(first, second)
	if len(got.Todos) != 1 || got.Todos[0].Content != "from-plan" {
		t.Fatalf("todos=%+v", got.Todos)
	}
}
