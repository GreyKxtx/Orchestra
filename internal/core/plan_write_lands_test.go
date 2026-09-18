package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
)

// Does a plan written to .orchestra/plans/ land?
//
// plan_orders_two_files, live on the 27B (2026-09-18): the model wrote its
// plan to .orchestra/plans/<stamp>-plan.md with write and the tool refused
// five times in a row. This test was written to find out whether plan mode
// refuses that path at all — it does not; the plain create below lands. The
// real cause was a file_hash sent for a file that did not exist, and that
// message is fixed in fs.Write (TestFSWrite_HashForAMissingFile_SaysItIsMissing).
// The test stays: it pins that a plan-mode create at the assigned path is
// accepted end to end, which nothing else asserted.

type planScriptLLM struct {
	mu       sync.Mutex
	calls    int
	toolSaid string
}

func (s *planScriptLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (s *planScriptLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.calls == 1 {
		return toolCall("write", `{"path":".orchestra/plans/20260918-214201-plan.md","content":"# Plan\n\n1. internal/api/client.go: add a field.\n2. main.go: pass 3.\n"}`), nil
	}
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == llm.RoleTool {
			s.toolSaid = req.Messages[i].Content
			break
		}
	}
	return finalText("planned"), nil
}

func TestPlanMode_AWrittenPlanLands(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig(root)
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatal(err)
	}
	client := &planScriptLLM{}
	c, err := New(root, Options{LLMClient: client})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if _, err := c.AgentRun(context.Background(), AgentRunParams{
		Query: "plan the change",
		Mode:  "plan",
		Apply: true,
	}); err != nil {
		t.Fatalf("agent.run: %v", err)
	}
	t.Logf("the model was told: %s", client.toolSaid)
	if _, err := os.Stat(filepath.Join(root, ".orchestra", "plans", "20260918-214201-plan.md")); err != nil {
		t.Fatalf("the plan never landed (%v); the tool said: %s", err, client.toolSaid)
	}
	if strings.Contains(client.toolSaid, `"error"`) || strings.Contains(client.toolSaid, "denied") {
		t.Fatalf("write was refused: %s", client.toolSaid)
	}
}
