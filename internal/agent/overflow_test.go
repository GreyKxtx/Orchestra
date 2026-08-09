package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/protocol/schema"
	"github.com/orchestra/orchestra/internal/tools"
)

// overflowLLM mimics vLLM: any step whose prompt exceeds rejectAboveBytes is
// refused with a context-length 400. Compaction requests are always served.
type overflowLLM struct {
	rejectAboveBytes int
	stepCalls        int
	rejections       int
	compactionCalls  int
	rejectPromptTok  int
	rejectContextTok int
}

func (o *overflowLLM) Plan(ctx context.Context, prompt string) (string, error) { return "{}", nil }

func (o *overflowLLM) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	for _, m := range req.Messages {
		if m.Role == llm.RoleSystem && strings.Contains(m.Content, "Context Manager") {
			o.compactionCalls++
			return &llm.CompleteResponse{
				Message: llm.Message{Role: llm.RoleAssistant, Content: "Compacted summary."},
			}, nil
		}
	}
	o.stepCalls++
	promptBytes := 0
	for _, m := range req.Messages {
		promptBytes += m.TextLen()
	}
	if promptBytes > o.rejectAboveBytes {
		o.rejections++
		return nil, fmt.Errorf("request failed (status 400): This model's maximum context length is %d tokens. "+
			"However, you requested 8192 output tokens and your prompt contains at least %d input tokens, "+
			"for a total of at least %d tokens.",
			o.rejectContextTok, o.rejectPromptTok, o.rejectPromptTok+8192)
	}
	return &llm.CompleteResponse{
		Message: llm.Message{Role: llm.RoleAssistant, Content: `{"type":"final","final":{"patches":[]}}`},
		Usage:   &llm.TokenUsage{PromptTokens: 1000, CompletionTokens: 10, TotalTokens: 1010},
	}, nil
}

func newOverflowAgent(t *testing.T, client llm.Client) *Agent {
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
	ag, err := New(client, v, runner, Options{
		MaxSteps:            6,
		MaxPromptBytes:      256 * 1024,
		CompactThresholdPct: 100, // don't pre-empt: let the provider 400 drive recovery
		ModelContextTokens:  51200,
		CompletionMaxTokens: 8192,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return ag
}

func bigHistory(msgs, size int) []llm.Message {
	out := make([]llm.Message, 0, msgs)
	for i := 0; i < msgs; i++ {
		role := llm.RoleUser
		if i%2 == 1 {
			role = llm.RoleAssistant
		}
		out = append(out, llm.Message{Role: role, Content: strings.Repeat("x", size)})
	}
	return out
}

func TestRun_ContextOverflowCompactsAndRetries(t *testing.T) {
	client := &overflowLLM{rejectAboveBytes: 60000, rejectContextTok: 51200, rejectPromptTok: 48000}
	ag := newOverflowAgent(t, client)

	_, res, err := ag.Run(context.Background(), bigHistory(20, 4000), "do something")
	if err != nil {
		t.Fatalf("Run returned error instead of recovering: %v", err)
	}
	if res == nil {
		t.Fatal("expected a result after overflow recovery")
	}
	if client.compactionCalls == 0 {
		t.Error("expected compaction to run on overflow")
	}
	if client.stepCalls < 2 {
		t.Errorf("expected the step to be retried, stepCalls=%d", client.stepCalls)
	}
	if ag.overflowRecoveries != 1 {
		t.Errorf("overflowRecoveries = %d, want 1", ag.overflowRecoveries)
	}
}

func TestRun_ContextOverflowGivesUpAfterBudget(t *testing.T) {
	client := &overflowLLM{rejectAboveBytes: 0, rejectContextTok: 51200, rejectPromptTok: 48000}
	ag := newOverflowAgent(t, client)

	_, _, err := ag.Run(context.Background(), bigHistory(20, 4000), "do something")
	if err == nil {
		t.Fatal("expected the turn to fail once the recovery budget is spent")
	}
	if !llm.IsContextOverflowError(err) {
		t.Fatalf("expected the overflow error to surface, got %v", err)
	}
	if ag.overflowRecoveries > maxOverflowRecoveries {
		t.Errorf("overflowRecoveries = %d, want ≤ %d", ag.overflowRecoveries, maxOverflowRecoveries)
	}
}

func TestRecoverFromOverflow_LearnsContextWindow(t *testing.T) {
	client := &overflowLLM{rejectAboveBytes: 1 << 30}
	ag := newOverflowAgent(t, client)
	ag.opts.ModelContextTokens = 128000 // stale config value

	err := fmt.Errorf("request failed (status 400): This model's maximum context length is 51200 tokens. " +
		"However, you requested 8192 output tokens and your prompt contains at least 47000 input tokens, " +
		"for a total of at least 55192 tokens.")
	hist, ok := ag.recoverFromOverflow(context.Background(), "q", bigHistory(20, 4000), err, 1)
	if !ok {
		t.Fatal("expected recovery to succeed")
	}
	if ag.opts.ModelContextTokens != 51200 {
		t.Errorf("ModelContextTokens = %d, want 51200 (learned from provider)", ag.opts.ModelContextTokens)
	}
	if ag.lastPromptTokens != 47000 {
		t.Errorf("lastPromptTokens = %d, want 47000", ag.lastPromptTokens)
	}
	if got, target := historyBytes(hist), ag.overflowTargetBytes(); got > target {
		t.Errorf("history %d bytes exceeds overflow target %d", got, target)
	}
}

func TestCalibrateFromRealPrompt_PrefersPessimistic(t *testing.T) {
	ag := &Agent{opts: Options{BytesPerContextToken: 4}}
	ag.lastPromptBytes = 30000
	ag.calibrateFromRealPrompt(10000) // 3 bytes/token in reality
	if got := ag.bytesPerToken(); got != 3 {
		t.Fatalf("bytesPerToken = %d, want 3 after calibration", got)
	}
	ag.lastPromptBytes = 40000
	ag.calibrateFromRealPrompt(8000) // 5 bytes/token — less pessimistic, ignored
	if got := ag.bytesPerToken(); got != 3 {
		t.Fatalf("bytesPerToken = %d, want the pessimistic 3 to stick", got)
	}
}

func TestCompactionCorpusBudget_PrefersCompactionContextTokens(t *testing.T) {
	// Main model has a huge window; the fast compaction provider has a much
	// smaller one. The corpus budget must track the model that will actually
	// receive it, not the main model.
	agMainOnly := &Agent{opts: Options{MaxPromptBytes: 1 << 20, ModelContextTokens: 200000, CompletionMaxTokens: 4096}}
	agWithCompaction := &Agent{opts: Options{
		MaxPromptBytes:          1 << 20,
		ModelContextTokens:      200000,
		CompactionContextTokens: 8192,
		CompletionMaxTokens:     4096,
	}}
	big := agMainOnly.compactionCorpusBudget()
	small := agWithCompaction.compactionCorpusBudget()
	if small >= big {
		t.Fatalf("compactionCorpusBudget with CompactionContextTokens=8192 (%d) should be smaller than main-model-only budget (%d)", small, big)
	}
}

func TestBuildCompactionCorpus_FitsBudget(t *testing.T) {
	ag := &Agent{opts: Options{MaxPromptBytes: 32 * 1024, ModelContextTokens: 8192, CompletionMaxTokens: 1024}}
	corpus := ag.buildCompactionCorpus(bigHistory(50, 5000))
	if len(corpus) > ag.compactionCorpusBudget() {
		t.Fatalf("corpus %d bytes exceeds budget %d", len(corpus), ag.compactionCorpusBudget())
	}
	if !strings.Contains(corpus, "earlier messages omitted") {
		t.Error("expected an omission marker when history does not fit")
	}
	if !strings.Contains(corpus, "[clipped]") {
		t.Error("expected oversized entries to be clipped")
	}
}
