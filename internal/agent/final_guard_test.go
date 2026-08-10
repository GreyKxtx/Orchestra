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
		{"Распиши что там реализованно и сделанно", nil, ModeBuild, false},
		{"оставь в конце коментарий", nil, ModeBuild, true},
		{"посмотри какой комментарий в конце файла app.jsx", nil, ModeBuild, false},
		{"what comment is at the end of the file", nil, ModeBuild, false},
		{"добавь комментарий в конец файла", nil, ModeBuild, true},
		{"what was implemented in this module", nil, ModeBuild, false},
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

func TestRejectPrematureFinal_step1PatchesOnly_conversationalAllowed(t *testing.T) {
	a := &Agent{opts: Options{Mode: ModeBuild}}
	step := &Step{Type: StepFinal, Final: &Final{Patches: nil}}
	_, reject := a.rejectPrematureFinal("explain this function", step, `{"patches":[]}`, 1)
	if reject {
		t.Fatal("patches-only final on step 1 should be allowed for non-code queries")
	}
}

func TestRejectPrematureFinal_step1PatchesOnly_codeTaskRejected(t *testing.T) {
	a := &Agent{opts: Options{Mode: ModeBuild}}
	step := &Step{Type: StepFinal, Final: &Final{Patches: nil}}
	hint, reject := a.rejectPrematureFinal("fix the search bug", step, `{"patches":[]}`, 1)
	if !reject {
		t.Fatal("patches-only final on step 1 should be rejected for code-change queries")
	}
	if !strings.Contains(hint, "without tool calls") {
		t.Fatalf("hint=%q", hint)
	}
}

func TestRejectPrematureFinal_readOnlyCommentQueryAfterRead(t *testing.T) {
	a := &Agent{
		opts:              Options{Mode: ModeBuild},
		turnMutatingTools: 0, // read only this turn
	}
	step := &Step{Type: StepFinal, Final: &Final{Patches: nil}}
	query := "посмотри какой комментарий в конце файла app.jsx"
	answer := "В конце файла находится комментарий: // тестик"
	_, reject := a.rejectPrematureFinal(query, step, answer, 2)
	if reject {
		t.Fatal("read-only comment question should allow plain-text final after read")
	}
}

