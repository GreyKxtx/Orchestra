package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/agent/working"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/schema"
	"github.com/orchestra/orchestra/internal/tools"
)

func TestHistoryBytes_CountsContent(t *testing.T) {
	history := []llm.Message{
		{Role: llm.RoleUser, Content: "hello"},
		{Role: llm.RoleAssistant, Content: "world"},
	}
	got := historyBytes(history)
	want := estimateMessageSize(history[0]) + estimateMessageSize(history[1])
	if got != want {
		t.Errorf("historyBytes = %d, want %d", got, want)
	}
}

func TestHistoryBytes_CountsToolCallArgs(t *testing.T) {
	args := `{"path":"main.go"}`
	history := []llm.Message{
		{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{
				{
					Function: llm.ToolCallFunc{
						Name:      "read",
						Arguments: llm.ToolArguments([]byte(args)),
					},
				},
			},
		},
	}
	got := historyBytes(history)
	want := estimateMessageSize(history[0])
	if got != want {
		t.Errorf("historyBytes = %d, want %d", got, want)
	}
	if got <= len("read")+len(args) {
		t.Errorf("historyBytes should include estimate overhead, got %d", got)
	}
}

// compactionLLM returns a fixed summary when called in compaction mode.
type compactionLLM struct {
	compactionCalled bool
	scriptedLLM
}

func (c *compactionLLM) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	// Detect compaction call by checking for the compaction system prompt marker.
	for _, m := range req.Messages {
		if m.Role == llm.RoleSystem && strings.Contains(m.Content, "Context Manager") {
			c.compactionCalled = true
			return &llm.CompleteResponse{
				Message: llm.Message{Role: llm.RoleAssistant, Content: "Compacted summary."},
			}, nil
		}
	}
	return c.scriptedLLM.Complete(ctx, req)
}

// buildCorpusFixture returns an old tool-call entry referencing targetPath
// followed by enough filler history that the old entry would normally fall
// outside compactionCorpusBudget's oldest-first cut.
func buildCorpusFixture(targetPath string) []llm.Message {
	hist := []llm.Message{
		{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				Function: llm.ToolCallFunc{
					Name:      "read",
					Arguments: llm.ToolArguments(json.RawMessage(`{"path":"` + targetPath + `"}`)),
				},
			}},
		},
	}
	hist = append(hist, bigHistory(50, 3000)...)
	return hist
}

func TestBuildCompactionCorpus_RescuesActiveFile(t *testing.T) {
	const target = "internal/agent/agent.go"
	hist := buildCorpusFixture(target)

	// Baseline: no working state → the old entry about target is dropped.
	agNoWorking := &Agent{opts: Options{MaxPromptBytes: 32 * 1024, ModelContextTokens: 8192, CompletionMaxTokens: 1024}}
	baseline := agNoWorking.buildCompactionCorpus(hist)
	if strings.Contains(baseline, target) {
		t.Fatalf("test fixture invalid: baseline corpus already contains %q without rescue", target)
	}

	// With working state marking target as active, it must survive the cut.
	agWithWorking := &Agent{opts: Options{MaxPromptBytes: 32 * 1024, ModelContextTokens: 8192, CompletionMaxTokens: 1024}}
	agWithWorking.working = working.New("fix bug")
	agWithWorking.working.ObserveTool("read", json.RawMessage(`{"path":"`+target+`"}`), []byte(`{}`), nil)

	rescued := agWithWorking.buildCompactionCorpus(hist)
	if !strings.Contains(rescued, target) {
		t.Errorf("expected active file %q to be rescued from the omitted range, corpus: %s", target, clipChars(rescued, 400))
	}
}

func TestCompactHistory_ReturnsCompactedHistory(t *testing.T) {
	dir := t.TempDir()
	runner, err := tools.NewRunner(dir, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer runner.Close()

	llmClient := &compactionLLM{
		scriptedLLM: scriptedLLM{
			steps: []string{
				`{"type":"final","final":{"patches":[]}}`,
			},
		},
	}

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	ag, err := New(llmClient, v, runner, Options{
		MaxSteps:            5,
		MaxPromptBytes:      1000,
		CompactThresholdPct: 1, // trigger immediately (1% of 1000 = 10 bytes)
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Build a history that exceeds the threshold.
	bigHistory := []llm.Message{
		{Role: llm.RoleUser, Content: strings.Repeat("a", 50)},
		{Role: llm.RoleAssistant, Content: strings.Repeat("b", 50)},
	}

	compacted, err := ag.compactHistory(context.Background(), "test query", bigHistory)
	if err != nil {
		t.Fatalf("compactHistory error: %v", err)
	}
	if !llmClient.compactionCalled {
		t.Error("expected compaction LLM call to have been made")
	}
	if len(compacted) < 1 {
		t.Errorf("expected compacted history, got %d", len(compacted))
	}
	if !strings.Contains(compacted[0].Content, "Compacted summary") {
		t.Errorf("compacted message should contain summary, got: %q", compacted[0].Content)
	}
	if !strings.Contains(compacted[0].Content, "Session checkpoint") {
		t.Errorf("expected sticky checkpoint header, got: %q", compacted[0].Content)
	}
}
