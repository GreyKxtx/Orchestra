package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// lifecycleRecorder is a HooksRunner that only remembers which lifecycle
// events it was asked about.
type lifecycleRecorder struct {
	events   []string
	payloads []string
}

func (r *lifecycleRecorder) RunPreTool(context.Context, string, json.RawMessage) HookDecision {
	return HookDecision{}
}

func (r *lifecycleRecorder) RunPostTool(context.Context, string, json.RawMessage) {}

func (r *lifecycleRecorder) RunLifecycle(_ context.Context, event string, payload json.RawMessage) HookDecision {
	r.events = append(r.events, event)
	r.payloads = append(r.payloads, string(payload))
	return HookDecision{}
}

// Compaction is the one moment the agent throws history away. A hook that
// wants to archive the transcript has to hear about it before that happens,
// not after.
func TestCompactHistory_FiresPreCompactHook(t *testing.T) {
	dir := t.TempDir()
	runner, err := tools.NewRunner(dir, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer runner.Close()

	llmClient := &compactionLLM{
		scriptedLLM: scriptedLLM{steps: []string{`{"type":"final","final":{"patches":[]}}`}},
	}
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	rec := &lifecycleRecorder{}
	ag, err := New(llmClient, v, runner, Options{
		MaxSteps:            5,
		MaxPromptBytes:      1000,
		CompactThresholdPct: 1,
		HooksRunner:         rec,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	hist := []llm.Message{
		{Role: llm.RoleUser, Content: strings.Repeat("a", 50)},
		{Role: llm.RoleAssistant, Content: strings.Repeat("b", 50)},
	}
	if _, err := ag.compactHistory(context.Background(), "test query", hist); err != nil {
		t.Fatalf("compactHistory: %v", err)
	}

	if len(rec.events) != 1 || rec.events[0] != "pre_compact" {
		t.Fatalf("expected one pre_compact event, got %v", rec.events)
	}
	if !strings.Contains(rec.payloads[0], `"messages":2`) {
		t.Fatalf("payload should say how much history is at stake, got %s", rec.payloads[0])
	}
}
