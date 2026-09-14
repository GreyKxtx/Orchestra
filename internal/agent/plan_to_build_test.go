package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// plan_exit and the plan → build switch had no end-to-end test of any kind.
// mergeAgentResults was covered; the path that produces the results it merges
// was not, and ContinueBuildAfterPlan had no caller in any test.
//
// The eval cannot cover it either: the switch happens only when a user
// approves, and there is no user in an eval run. That is what makes this test
// the only coverage the mechanism can have — and why the two plan tasks pass
// today without plan_exit ever being called.

// approvingAsker answers the one question plan_exit asks.
type approvingAsker struct {
	asked  int
	answer string
}

func (a *approvingAsker) Ask(ctx context.Context, questions []tools.QuestionItem) ([]string, error) {
	_ = ctx
	a.asked++
	if len(questions) == 0 {
		return nil, nil
	}
	return []string{a.answer}, nil
}

// planThenBuildLLM plans, asks to switch, then does the work in the second
// turn — the shape a real plan-mode session has.
type planThenBuildLLM struct {
	calls int
}

func (p *planThenBuildLLM) Plan(ctx context.Context, prompt string) (string, error) {
	_, _ = ctx, prompt
	return "{}", nil
}

func (p *planThenBuildLLM) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	_, _ = ctx, req
	p.calls++
	switch p.calls {
	case 1: // plan turn: write the plan
		return toolCallResponse("t1", "write", `{"path":".orchestra/plan.md","content":"# Plan\n\nChange Greet to say good morning.\n"}`), nil
	case 2: // plan turn: ask to switch
		return toolCallResponse("t2", "plan_exit", `{}`), nil
	case 3: // build turn: make the change the plan described
		return toolCallResponse("t3", "edit", `{"path":"greet.go","search":"\"hello\"","replace":"\"good morning\""}`), nil
	default: // build turn: finish
		return &llm.CompleteResponse{Message: llm.Message{
			Role:    llm.RoleAssistant,
			Content: `{"patches":[]}`,
		}}, nil
	}
}

func toolCallResponse(id, name, args string) *llm.CompleteResponse {
	return &llm.CompleteResponse{Message: llm.Message{
		Role: llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{
			ID: id, Type: "function",
			Function: llm.ToolCallFunc{Name: name, Arguments: llm.ToolArguments(args)},
		}},
	}}
}

func planWorkspace(t *testing.T) (*tools.Runner, *schema.Validator, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "greet.go"),
		[]byte("package main\n\nfunc Greet() string {\n\treturn \"hello\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { tr.Close() })
	return tr, v, root
}

func TestPlanExit_ApprovedPlanRunsTheBuildTurnAndTheChangeLands(t *testing.T) {
	tr, v, root := planWorkspace(t)
	client := &planThenBuildLLM{}
	asker := &approvingAsker{answer: "Yes, switch to build"}

	opts := Options{
		Mode:          ModePlan,
		MaxSteps:      8,
		Apply:         true,
		QuestionAsker: asker,
	}
	ag, err := New(client, v, tr, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	hist, res, err := ag.Run(context.Background(), nil, "make Greet say good morning")
	if err != nil {
		t.Fatalf("plan turn: %v", err)
	}
	if res == nil || !res.SwitchToBuild {
		t.Fatalf("an approved plan_exit must end the plan turn asking for build, got %+v", res)
	}
	if asker.asked != 1 {
		t.Errorf("plan_exit must ask exactly once, asked %d times", asker.asked)
	}

	_, merged, err := ContinueBuildAfterPlan(context.Background(), client, v, tr, opts, hist, res)
	if err != nil {
		t.Fatalf("build turn: %v", err)
	}
	if merged == nil {
		t.Fatal("the merged result is nil")
	}
	if merged.SwitchToBuild {
		t.Error("the merged result still asks to switch; the second turn already happened")
	}

	body, err := os.ReadFile(filepath.Join(root, "greet.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "good morning") {
		t.Errorf("the build turn's edit never reached disk, so the switch bought nothing:\n%s", body)
	}
	// The plan itself must survive: it is what the build turn was approved for.
	if _, err := os.Stat(filepath.Join(root, ".orchestra", "plan.md")); err != nil {
		t.Errorf("the plan written in the plan turn is gone: %v", err)
	}
}

// A refusal must leave the model planning, not end the turn — and above all
// must not run the build turn anyway.
func TestPlanExit_ARefusedSwitchKeepsPlanning(t *testing.T) {
	tr, v, root := planWorkspace(t)
	client := &planThenBuildLLM{}
	asker := &approvingAsker{answer: "No, keep planning"}

	ag, err := New(client, v, tr, Options{
		Mode:          ModePlan,
		MaxSteps:      6,
		Apply:         true,
		QuestionAsker: asker,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, res, err := ag.Run(context.Background(), nil, "make Greet say good morning")
	if err != nil {
		t.Fatalf("plan turn: %v", err)
	}
	if res != nil && res.SwitchToBuild {
		t.Fatal("a refused plan_exit must not ask for build mode")
	}

	body, err := os.ReadFile(filepath.Join(root, "greet.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "good morning") {
		t.Error("plan mode edited code after the switch was refused")
	}
}

// Without an asker there is nobody to approve, and the tool says so rather
// than switching on its own.
func TestPlanExit_WithNoAskerRefusesRatherThanSwitching(t *testing.T) {
	tr, v, _ := planWorkspace(t)

	ag, err := New(&planThenBuildLLM{}, v, tr, Options{
		Mode:     ModePlan,
		MaxSteps: 6,
		Apply:    true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, res, err := ag.Run(context.Background(), nil, "make Greet say good morning")
	if err != nil {
		t.Fatalf("plan turn: %v", err)
	}
	if res != nil && res.SwitchToBuild {
		t.Fatal("a non-interactive run switched to build with nobody to approve it")
	}
}
