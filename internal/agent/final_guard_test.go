package agent

import (
	"testing"

	"github.com/orchestra/orchestra/internal/llm"
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
		{"", []tools.TodoItem{{ID: "1", Content: "x", Status: tools.TodoInProgress}}, ModeBuild, true},
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

func TestRejectPrematureFinal_actionQuery(t *testing.T) {
	a := &Agent{
		opts: Options{Mode: ModeBuild},
	}
	step := &Step{Type: StepFinal, Final: &Final{Patches: nil}}
	hint, reject := a.rejectPrematureFinal("fix search in script.js", step, `{"patches":[]}`, 2)
	if !reject {
		t.Fatal("expected reject for empty patches on fix query")
	}
	if hint == "" {
		t.Fatal("expected hint")
	}
}
