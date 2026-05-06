package agent

import (
	"context"
	"strings"
	"testing"

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
	want := len("hello") + len("world")
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
	want := len("read") + len(args)
	if got != want {
		t.Errorf("historyBytes = %d, want %d", got, want)
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
		if m.Role == llm.RoleSystem && strings.Contains(m.Content, "сжать историю") {
			c.compactionCalled = true
			return &llm.CompleteResponse{
				Message: llm.Message{Role: llm.RoleAssistant, Content: "Compacted summary."},
			}, nil
		}
	}
	return c.scriptedLLM.Complete(ctx, req)
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
	if len(compacted) != 1 {
		t.Errorf("expected 1 compacted message, got %d", len(compacted))
	}
	if !strings.Contains(compacted[0].Content, "Compacted summary") {
		t.Errorf("compacted message should contain summary, got: %q", compacted[0].Content)
	}
}
