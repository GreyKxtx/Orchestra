package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// deadLLM is a provider that is configured but not listening — the usual state
// of a "fast" endpoint on a machine that is currently off.
type deadLLM struct {
	scriptedLLM
	calls int
}

func (d *deadLLM) Complete(context.Context, llm.CompleteRequest) (*llm.CompleteResponse, error) {
	d.calls++
	return nil, &llm.UnreachableError{
		Endpoint: "http://10.5.0.2:1234/v1",
		Err:      errors.New("dial tcp: no connection could be made"),
	}
}

func newCompactionTestAgent(t *testing.T, main llm.Client, opts Options) *Agent {
	t.Helper()
	runner, err := tools.NewRunner(t.TempDir(), tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { runner.Close() })
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	opts.MaxSteps = 5
	if opts.MaxPromptBytes == 0 {
		opts.MaxPromptBytes = 1000
	}
	opts.CompactThresholdPct = 1
	ag, err := New(main, v, runner, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return ag
}

func compactionFixture() []llm.Message {
	return []llm.Message{
		{Role: llm.RoleUser, Content: strings.Repeat("a", 50)},
		{Role: llm.RoleAssistant, Content: strings.Repeat("b", 50)},
	}
}

// Compaction is routed to the cheap "fast" provider. When that provider is
// down but the model actually answering the turn is alive, the turn must not
// die: summarising with the main model costs more tokens than the cheap one,
// and is still far better than losing the user's turn to an optional helper.
func TestCompactHistory_FallsBackToTheMainModelWhenTheFastOneIsDown(t *testing.T) {
	main := &compactionLLM{scriptedLLM: scriptedLLM{steps: []string{`{"type":"final","final":{"patches":[]}}`}}}
	fast := &deadLLM{}
	ag := newCompactionTestAgent(t, main, Options{CompactionClient: fast})

	compacted, err := ag.compactHistory(context.Background(), "test query", compactionFixture())
	if err != nil {
		t.Fatalf("a dead fast provider must not fail compaction: %v", err)
	}
	if fast.calls == 0 {
		t.Fatal("the fast provider must still be tried first")
	}
	if !main.compactionCalled {
		t.Fatal("the main model must answer when the fast provider is unreachable")
	}
	if len(compacted) == 0 || !strings.Contains(compacted[0].Content, "Compacted summary") {
		t.Fatalf("expected a real checkpoint, got %#v", compacted)
	}
	// The main model is alive, so nothing may mark the run's LLM as dead —
	// that flag aborts the whole turn and blocks every later recovery.
	if ag.llmInfraErr != nil {
		t.Fatalf("a dead helper must not mark the main LLM unreachable: %v", ag.llmInfraErr)
	}
}

// The opposite case still has to fail: when the model answering the turn is
// itself unreachable there is nothing left to fall back to.
func TestCompactHistory_MainModelUnreachableStopsTheRun(t *testing.T) {
	main := &deadLLM{}
	ag := newCompactionTestAgent(t, main, Options{})

	if _, err := ag.compactHistory(context.Background(), "test query", compactionFixture()); err == nil {
		t.Fatal("compaction must fail when the main model is unreachable")
	}
	if ag.llmInfraErr == nil {
		t.Fatal("an unreachable main model must be recorded so the run stops")
	}
}

// And a fast provider that answers is still the one that does the work.
func TestCompactHistory_UsesTheFastProviderWhenItAnswers(t *testing.T) {
	main := &compactionLLM{scriptedLLM: scriptedLLM{steps: []string{`{"type":"final","final":{"patches":[]}}`}}}
	fast := &compactionLLM{scriptedLLM: scriptedLLM{steps: []string{`{"type":"final","final":{"patches":[]}}`}}}
	ag := newCompactionTestAgent(t, main, Options{CompactionClient: fast})

	if _, err := ag.compactHistory(context.Background(), "test query", compactionFixture()); err != nil {
		t.Fatalf("compactHistory: %v", err)
	}
	if !fast.compactionCalled {
		t.Fatal("the fast provider must do the work when it is reachable")
	}
	if main.compactionCalled {
		t.Fatal("the main model must not be called when the fast provider answered")
	}
}
