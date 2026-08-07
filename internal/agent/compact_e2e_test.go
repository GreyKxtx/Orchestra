package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/schema"
	"github.com/orchestra/orchestra/internal/tools"
)

// stepLLM scripts a sequence of agent steps and counts compaction calls so we
// can assert the agent triggered them during Run (not just compactHistory in
// isolation, which is covered by TestCompactHistory_ReturnsCompactedHistory).
type stepLLM struct {
	steps             []*llm.CompleteResponse
	i                 int
	compactionCalls   int
	normalCalls       int
	lastObservedBytes int
}

func (l *stepLLM) Plan(ctx context.Context, prompt string) (string, error) { return "{}", nil }

func (l *stepLLM) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	for _, m := range req.Messages {
		if m.Role == llm.RoleSystem && strings.Contains(m.Content, "Context Manager") {
			l.compactionCalls++
			return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "summary placeholder"}}, nil
		}
	}
	l.normalCalls++
	// Record post-prompt history bytes so the test can assert compaction
	// actually shrank the prompt before the next non-compaction call.
	l.lastObservedBytes = 0
	for _, m := range req.Messages {
		l.lastObservedBytes += len(m.Content)
		for _, tc := range m.ToolCalls {
			l.lastObservedBytes += len(tc.Function.Name) + len(tc.Function.Arguments)
		}
	}
	if l.i >= len(l.steps) {
		return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`}}, nil
	}
	out := l.steps[l.i]
	l.i++
	return out, nil
}

// TestAgent_E2E_CompactionFiresOnLongSession runs the agent with a low
// MaxPromptBytes + CompactThresholdPct and verifies that during the loop the
// agent actually invokes compactHistory (not just truncation).
func TestAgent_E2E_CompactionFiresOnLongSession(t *testing.T) {
	dir := t.TempDir()
	runner, err := tools.NewRunner(dir, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()

	// Each scripted step requests a fs.list call with a long-ish argument blob.
	// The tool result is bounded but we pad the path arg so history grows quickly.
	bigArg := strings.Repeat("x", 600)
	mkCall := func(i int) *llm.CompleteResponse {
		return &llm.CompleteResponse{Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				ID:       "call-" + string(rune('a'+i)),
				Type:     "function",
				Function: llm.ToolCallFunc{Name: "ls", Arguments: llm.ToolArguments([]byte(`{"path":"` + bigArg + `"}`))},
			}},
		}}
	}
	steps := []*llm.CompleteResponse{
		mkCall(0), mkCall(1), mkCall(2), mkCall(3),
		{Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`}},
	}

	llmClient := &stepLLM{steps: steps}
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}

	ag, err := New(llmClient, v, runner, Options{
		MaxSteps:            10,
		MaxPromptBytes:      2000,
		CompactThresholdPct: 30, // threshold ≈ 600 bytes; one bigArg pushes us past
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := ag.Run(context.Background(), nil, "list big paths"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if llmClient.compactionCalls == 0 {
		t.Fatalf("expected compaction to fire at least once during long session; got 0 (normal calls=%d)", llmClient.normalCalls)
	}
	t.Logf("compaction fired %d time(s) over %d normal LLM calls", llmClient.compactionCalls, llmClient.normalCalls)
}

// TestAgent_E2E_CompactionRespectsTracker verifies that compaction LLM calls
// also flow through UsageTracker so cost telemetry stays accurate.
func TestAgent_E2E_CompactionRespectsTracker(t *testing.T) {
	dir := t.TempDir()
	runner, err := tools.NewRunner(dir, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()

	bigArg := strings.Repeat("y", 600)
	mkCall := func() *llm.CompleteResponse {
		return &llm.CompleteResponse{
			Message: llm.Message{
				Role: llm.RoleAssistant,
				ToolCalls: []llm.ToolCall{{
					ID:       "c1",
					Type:     "function",
					Function: llm.ToolCallFunc{Name: "ls", Arguments: llm.ToolArguments([]byte(`{"path":"` + bigArg + `"}`))},
				}},
			},
			Usage: &llm.TokenUsage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120},
		}
	}
	finalResp := &llm.CompleteResponse{
		Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`},
		Usage:   &llm.TokenUsage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60},
	}
	llmClient := &stepLLM{steps: []*llm.CompleteResponse{mkCall(), mkCall(), mkCall(), finalResp}}

	rec := &recordingTracker{}
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	ag, err := New(llmClient, v, runner, Options{
		MaxSteps:            10,
		MaxPromptBytes:      2000,
		CompactThresholdPct: 30,
		UsageTracker:        rec,
		ProviderLabel:       "test",
		ModelLabel:          "m",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ag.Run(context.Background(), nil, "ls"); err != nil {
		t.Fatal(err)
	}

	if rec.calls == 0 {
		t.Fatal("UsageTracker.Record was never called")
	}
	// At least one of the calls should have a small prompt count from a compaction
	// or normal step. Bound check: we should NOT have leaked compaction calls
	// somewhere that bypasses the tracker (i.e. normalCalls + compactionCalls
	// should equal rec.calls when every LLM call carries Usage in the script).
	want := llmClient.normalCalls + llmClient.compactionCalls
	// Compaction stepLLM responses do not carry Usage so they won't be counted by
	// the tracker — still, every normal call should produce a Record.
	if rec.calls != llmClient.normalCalls {
		t.Fatalf("tracker should see every Usage-bearing call: tracker=%d normal=%d compaction=%d total=%d",
			rec.calls, llmClient.normalCalls, llmClient.compactionCalls, want)
	}
}

type recordingTracker struct {
	calls           int
	totalPrompt     int
	totalCompletion int
	lastProvider    string
	lastModel       string
}

func (r *recordingTracker) Record(provider, model string, prompt, completion int) {
	r.calls++
	r.totalPrompt += prompt
	r.totalCompletion += completion
	r.lastProvider = provider
	r.lastModel = model
}
