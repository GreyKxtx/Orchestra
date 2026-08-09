package agent

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/internal/tools"
)

func TestQueryRequiresCodeChanges(t *testing.T) {
	tests := []struct {
		query string
		todos []tools.TodoItem
		mode  Mode
		want  bool
	}{
		{"explain this function", nil, ModeBuild, false},
		{"fix the search bug", nil, ModeBuild, true},
		{"население не грузится, поиск не работает", nil, ModeBuild, true},
		{"hello", nil, ModeExplore, false},
		{"implement feature", nil, ModeExplore, false},
		{"fix the bug", nil, ModeAsk, false},
		{"исправь поиск", nil, ModePlan, false},
		{"add feature", nil, ModeArchitecture, false},
		{"fix login", nil, ModeOrchestra, false},
		{"", []tools.TodoItem{{ID: "1", Content: "x", Status: tools.TodoInProgress}}, ModeBuild, true},
		{"", []tools.TodoItem{{ID: "1", Content: "x", Status: tools.TodoInProgress}}, ModeAsk, false},
	}
	for _, tt := range tests {
		got := queryRequiresCodeChanges(tt.query, tt.todos, tt.mode)
		if got != tt.want {
			t.Errorf("queryRequiresCodeChanges(%q) = %v, want %v", tt.query, got, tt.want)
		}
	}
}

func TestIsContentOnlyPatchesJSON(t *testing.T) {
	if !isContentOnlyPatchesJSON(`<think>done</think>{"patches":[]}`) {
		t.Fatal("expected patches-only JSON")
	}
	if isContentOnlyPatchesJSON("Here is the fix.\n{\"patches\":[]}") {
		t.Fatal("prose + patches should not be patches-only")
	}
}

func TestNormalizeLLM_loosePatchesJSON(t *testing.T) {
	resp := &llm.CompleteResponse{
		Message: llm.Message{Content: `{"patches":[]}`},
	}
	step, _, err := NormalizeLLM(nil, resp)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if step.Type != StepFinal || step.Final == nil {
		t.Fatalf("step: %+v", step)
	}
	if len(step.Final.Patches) != 0 {
		t.Fatalf("patches: %v", step.Final.Patches)
	}
}

func TestRejectPrematureFinal_openTodosBlockAfterEdit(t *testing.T) {
	a := &Agent{
		opts: Options{Mode: ModeBuild},
		todos: []tools.TodoItem{
			{ID: "1", Content: "edit pkg", Status: tools.TodoInProgress},
			{ID: "2", Content: "more work", Status: tools.TodoPending},
		},
		turnMutatingTools: 1, // already edited once — old bug allowed final here
	}
	step := &Step{Type: StepFinal, Final: &Final{Patches: nil}}
	hint, reject := a.rejectPrematureFinal("перейди на maplibre", step, "done\n{\"patches\":[]}", 5)
	if !reject {
		t.Fatal("expected reject while open todos remain after an edit")
	}
	if !strings.Contains(hint, "open todo") {
		t.Fatalf("hint=%q", hint)
	}
}

func TestRejectPrematureFinal_allowsFinalWhenTodosDone(t *testing.T) {
	a := &Agent{
		opts: Options{Mode: ModeBuild},
		todos: []tools.TodoItem{
			{ID: "1", Content: "edit pkg", Status: tools.TodoDone},
		},
		turnMutatingTools: 1,
	}
	step := &Step{Type: StepFinal, Final: &Final{Patches: nil}}
	_, reject := a.rejectPrematureFinal("перейди на maplibre", step, "all done\n{\"patches\":[]}", 5)
	if reject {
		t.Fatal("completed todos should allow final after edits")
	}
}

