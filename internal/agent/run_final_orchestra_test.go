package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/patches"
	"github.com/orchestra/orchestra/protocol/schema"
	"github.com/orchestra/orchestra/internal/tools"
)

func TestHandleFinalStep_OrchestraBlocksProductionPatches(t *testing.T) {
	root := t.TempDir()
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	r, err := tools.NewRunner(root, tools.RunnerOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })

	ag, err := New(finalStubLLM{}, v, r, Options{
		Mode:     ModeOrchestra,
		MaxSteps: 3,
	})
	if err != nil {
		t.Fatal(err)
	}

	step := &Step{
		Type: StepFinal,
		Final: &Final{
			Patches: []patches.Patch{{
				Type:    patches.TypeFileSearchReplace,
				Path:    "main.go",
				Search:  "x",
				Replace: "y",
			}},
		},
	}
	history := []llm.Message{}
	cb := NewCircuitBreaker(2, 6, 6, 3)
	emit := func(string) {}

	out, err := ag.handleFinalStep(context.Background(), cb, &history, step, nil, 1, "", emit)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Retry {
		t.Fatalf("expected retry, got %+v", out)
	}
	if len(history) == 0 || !strings.Contains(history[0].Content, "task(subagent_type=worker") {
		t.Fatalf("history: %+v", history)
	}
}

type finalStubLLM struct{}

func (finalStubLLM) Complete(context.Context, llm.CompleteRequest) (*llm.CompleteResponse, error) {
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "ok"}}, nil
}

func (finalStubLLM) Plan(context.Context, string) (string, error) { return "{}", nil }
