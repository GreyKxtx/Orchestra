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

// systemPromptRecorder records the system prompt of every request, so a block
// that is present on step 1 and gone on step 2 is visible.
type systemPromptRecorder struct {
	prompts []string
}

func (l *systemPromptRecorder) Plan(context.Context, string) (string, error) { return "{}", nil }

func (l *systemPromptRecorder) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	for _, m := range req.Messages {
		if m.Role == llm.RoleSystem {
			l.prompts = append(l.prompts, m.Content)
			break
		}
	}
	if len(l.prompts) == 1 {
		return &llm.CompleteResponse{Message: llm.Message{
			Role:    llm.RoleAssistant,
			Content: `{"type":"tool_call","tool":{"name":"bash","input":{"command":"echo","args":["hi"]}}}`,
		}}, nil
	}
	return &llm.CompleteResponse{Message: llm.Message{
		Role:    llm.RoleAssistant,
		Content: `{"type":"final","final":{"patches":[]}}`,
	}}, nil
}

// An @-mention states which code the turn is about. Its package rules used to
// load only when a tool read the file, so they arrived a step late — or never,
// when the model answered from the mention alone.
func TestRun_MentionedPackageRulesReachTheSystemPrompt(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "pkg", "auth")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "ORCHESTRA.md"),
		[]byte("AUTH PACKAGE RULES"), 0o644); err != nil {
		t.Fatal(err)
	}

	runner, err := tools.NewRunner(dir, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	client := &systemPromptRecorder{}
	ag, err := New(client, v, runner, Options{MaxSteps: 4})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ag.Run(context.Background(), nil, "what does @pkg/auth/token.go do?"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(client.prompts) < 2 {
		t.Fatalf("the run took %d LLM calls; the test needs two to check step 2", len(client.prompts))
	}
	for i, p := range client.prompts {
		if !strings.Contains(p, "AUTH PACKAGE RULES") {
			t.Errorf("step %d's system prompt lost the mentioned package's rules.\n"+
				"They are computed once per Run precisely because discoverInstructions "+
				"dedupes per directory — recomputing per step returns nothing the second "+
				"time and the rules drop out mid-turn.", i+1)
		}
	}
}

// No mention, no block: an ordinary question must not grow the system prompt.
func TestRun_NoMentionAddsNothing(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "pkg", "auth")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "ORCHESTRA.md"),
		[]byte("AUTH PACKAGE RULES"), 0o644); err != nil {
		t.Fatal(err)
	}

	runner, err := tools.NewRunner(dir, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	client := &systemPromptRecorder{}
	ag, err := New(client, v, runner, Options{MaxSteps: 4})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ag.Run(context.Background(), nil, "how are you?"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for i, p := range client.prompts {
		if strings.Contains(p, "AUTH PACKAGE RULES") {
			t.Errorf("step %d pulled in an unrelated package's rules with no mention", i+1)
		}
	}
}
