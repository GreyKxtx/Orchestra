package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// prefixRecorderLLM records the leading (cacheable) messages of every request
// so a test can assert they stay byte-identical across steps.
type prefixRecorderLLM struct {
	steps    []*llm.CompleteResponse
	i        int
	prefixes []string
	tails    []string
}

func (l *prefixRecorderLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (l *prefixRecorderLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	if len(req.Messages) >= 2 {
		l.prefixes = append(l.prefixes, req.Messages[0].Content+"\x00"+req.Messages[1].Content)
		l.tails = append(l.tails, req.Messages[len(req.Messages)-1].Content)
	}
	if l.i >= len(l.steps) {
		return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`}}, nil
	}
	out := l.steps[l.i]
	l.i++
	return out, nil
}

// TestAgent_PromptPrefixStableAcrossSteps guards the provider prompt cache:
// system + first user message must not change between steps, otherwise the
// cached prefix is invalidated on every call and the whole history is re-billed.
func TestAgent_PromptPrefixStableAcrossSteps(t *testing.T) {
	dir := t.TempDir()
	runner, err := tools.NewRunner(dir, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()

	mkCall := func(id string) *llm.CompleteResponse {
		return &llm.CompleteResponse{Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				ID:       id,
				Type:     "function",
				Function: llm.ToolCallFunc{Name: "ls", Arguments: llm.ToolArguments([]byte(`{"path":"."}`))},
			}},
		}}
	}
	client := &prefixRecorderLLM{steps: []*llm.CompleteResponse{
		mkCall("c1"), mkCall("c2"), mkCall("c3"),
		{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`}},
	}}

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	ag, err := New(client, v, runner, Options{MaxSteps: 8})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ag.Run(context.Background(), nil, "list the workspace"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(client.prefixes) < 3 {
		t.Fatalf("expected at least 3 recorded steps, got %d", len(client.prefixes))
	}
	// Step 1 legitimately differs (CKG context is injected once); from step 2
	// on the prefix must be stable.
	for i := 2; i < len(client.prefixes); i++ {
		if client.prefixes[i] != client.prefixes[1] {
			t.Fatalf("prompt prefix changed between step 2 and step %d — prompt cache would miss every step", i+1)
		}
	}
}

// TestAgent_VolatileBlockGoesLast keeps the working-state injection behind the
// history: in front of it, it breaks the cacheable prefix.
func TestAgent_VolatileBlockGoesLast(t *testing.T) {
	dir := t.TempDir()
	runner, err := tools.NewRunner(dir, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()

	mkCall := func(id string) *llm.CompleteResponse {
		return &llm.CompleteResponse{Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				ID:       id,
				Type:     "function",
				Function: llm.ToolCallFunc{Name: "ls", Arguments: llm.ToolArguments([]byte(`{"path":"."}`))},
			}},
		}}
	}
	client := &prefixRecorderLLM{steps: []*llm.CompleteResponse{mkCall("c1"), mkCall("c2")}}
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	on := true
	ag, err := New(client, v, runner, Options{MaxSteps: 4, WorkingState: &on})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ag.Run(context.Background(), nil, "do something"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(client.tails) < 2 {
		t.Fatalf("expected at least 2 LLM calls, got %d", len(client.tails))
	}
	// The block must actually be produced (otherwise this test is vacuous)…
	found := false
	for _, tail := range client.tails {
		if strings.Contains(tail, "<working_state>") {
			found = true
		}
	}
	if !found {
		t.Fatal("working state was never injected — test cannot prove where it lands")
	}
	// …and it must never sit in the cacheable prefix.
	for i, p := range client.prefixes {
		if strings.Contains(p, "<working_state>") {
			t.Fatalf("step %d: working state must not be in the leading user message", i+1)
		}
	}
}
