package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
	"github.com/orchestra/orchestra/internal/tools"
)

// panicLLM panics inside Complete — simulating a bug anywhere in the agent
// loop path that previously would have crashed the whole process when the
// agent ran inside a child-task goroutine.
type panicLLM struct{}

func (p *panicLLM) Plan(ctx context.Context, prompt string) (string, error) {
	_ = ctx
	_ = prompt
	return "{}", nil
}

func (p *panicLLM) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	panic("boom: simulated agent-loop panic")
}

// TestAgentRun_RecoversPanic verifies the resilience audit P1 contract:
// a panic escaping the agent loop surfaces as a regular error from Run
// instead of killing the process.
func TestAgentRun_RecoversPanic(t *testing.T) {
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(t.TempDir(), tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { tr.Close() })

	ag, err := New(&panicLLM{}, v, tr, Options{MaxSteps: 3})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	hist, res, err := ag.Run(context.Background(), []llm.Message{{Role: llm.RoleUser, Content: "prior"}}, "do something")
	if err == nil {
		t.Fatal("expected error from panicking run, got nil")
	}
	if !strings.Contains(err.Error(), "panicked") {
		t.Errorf("expected 'panicked' in error, got %q", err.Error())
	}
	if res != nil {
		t.Errorf("expected nil result after panic, got %+v", res)
	}
	// Best-effort contract: the input history is returned so the caller can
	// still persist the session up to the last completed turn.
	if len(hist) != 1 || hist[0].Content != "prior" {
		t.Errorf("expected original history back, got %+v", hist)
	}
}
