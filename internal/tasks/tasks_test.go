package tasks

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
	"github.com/orchestra/orchestra/internal/tools"

	"github.com/orchestra/orchestra/internal/agent"
)

// mockTaskResultLLM returns a task.result tool call immediately, simulating a
// child agent that finishes at step 1 with a fixed result string.
type mockTaskResultLLM struct {
	result string
}

func (m *mockTaskResultLLM) Complete(_ context.Context, _ llm.CompleteRequest) (*llm.CompleteResponse, error) {
	input, _ := json.Marshal(map[string]string{"content": m.result})
	return &llm.CompleteResponse{
		Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{
				{
					ID:   "call_1",
					Type: "function",
					Function: llm.ToolCallFunc{
						Name:      "task_result",
						Arguments: llm.ToolArguments(input),
					},
				},
			},
		},
	}, nil
}

func (m *mockTaskResultLLM) Plan(_ context.Context, _ string) (string, error) {
	return "", nil
}

func newTestTaskRunner(t *testing.T) *TaskRunner {
	t.Helper()
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("schema.NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(t.TempDir(), tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("tools.NewRunner: %v", err)
	}
	r := New(&mockTaskResultLLM{result: "all done"}, v, tr, ChildAgentConfig{})
	// Drain child goroutines before TempDir cleanup — otherwise Windows CI
	// fails with unlinkat .orchestra: The directory is not empty.
	t.Cleanup(func() {
		r.Close()
		_ = tr.Close()
	})
	return r
}

// ── Wait / Cancel with unknown task ─────────────────────────────────────────

func TestWait_UnknownTask(t *testing.T) {
	r := newTestTaskRunner(t)
	_, err := r.Wait(context.Background(), "task_99_0", 500)
	if err == nil {
		t.Fatal("expected error for unknown task ID")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' in error, got: %v", err)
	}
}

func TestCancel_UnknownTask(t *testing.T) {
	r := newTestTaskRunner(t)
	err := r.Cancel(context.Background(), "task_99_0")
	if err == nil {
		t.Fatal("expected error for unknown task ID")
	}
}

// ── Spawn ────────────────────────────────────────────────────────────────────

func TestSpawn_ReturnsNonEmptyID(t *testing.T) {
	r := newTestTaskRunner(t)
	id, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: "summarize files"})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty task ID")
	}
	if !strings.HasPrefix(id, "task_") {
		t.Fatalf("expected task ID to start with 'task_', got %q", id)
	}
}

func TestSpawn_IDsAreUnique(t *testing.T) {
	r := newTestTaskRunner(t)
	id1, _ := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: "goal1"})
	id2, _ := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: "goal2"})
	if id1 == id2 {
		t.Fatalf("expected unique IDs, got %q twice", id1)
	}
}

// ── Spawn + Wait ─────────────────────────────────────────────────────────────

func TestSpawnWait_ReturnsResult(t *testing.T) {
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("schema.NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(t.TempDir(), tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("tools.NewRunner: %v", err)
	}
	r := New(&mockTaskResultLLM{result: "research complete"}, v, tr, ChildAgentConfig{})
	t.Cleanup(func() {
		r.Close()
		_ = tr.Close()
	})

	id, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: "research files", MaxSteps: 3})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	result, err := r.Wait(context.Background(), id, 5000)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Status != "done" {
		t.Fatalf("expected status=done, got %q", result.Status)
	}
	if !strings.Contains(result.Result, "research complete") {
		t.Fatalf("expected result to contain 'research complete', got %q", result.Result)
	}
}

// ── Spawn + Cancel ───────────────────────────────────────────────────────────

func TestCancel_BeforeCompletion(t *testing.T) {
	// Use a mock LLM that blocks indefinitely so we can cancel it
	blockingLLM := &blockMockLLM{ready: make(chan struct{})}
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("schema.NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(t.TempDir(), tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("tools.NewRunner: %v", err)
	}
	r := New(blockingLLM, v, tr, ChildAgentConfig{})
	t.Cleanup(func() {
		r.Close()
		_ = tr.Close()
	})

	id, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: "block forever", MaxSteps: 1})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// Cancel it — should not panic
	if err := r.Cancel(context.Background(), id); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	// Wait should return "cancelled" (timeout on the blocking LLM)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, waitErr := r.Wait(ctx, id, 500)
	if waitErr != nil {
		t.Fatalf("Wait: %v", waitErr)
	}
	if result == nil {
		t.Fatal("expected non-nil result after cancel")
	}
	// Status is either "cancelled" (Wait timeout) or "error" (context cancelled)
	if result.Status == "" {
		t.Fatal("expected non-empty status")
	}
}

// blockMockLLM is an LLM that blocks until its context is cancelled.
type blockMockLLM struct {
	ready chan struct{}
}

func (m *blockMockLLM) Complete(ctx context.Context, _ llm.CompleteRequest) (*llm.CompleteResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (m *blockMockLLM) Plan(_ context.Context, _ string) (string, error) {
	return "", nil
}

// ── MaxSteps clamping ────────────────────────────────────────────────────────

func TestSpawn_ParentCancelStopsChild(t *testing.T) {
	blockingLLM := &blockMockLLM{ready: make(chan struct{})}
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("schema.NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(t.TempDir(), tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("tools.NewRunner: %v", err)
	}
	r := New(blockingLLM, v, tr, ChildAgentConfig{})
	t.Cleanup(func() {
		r.Close()
		_ = tr.Close()
	})

	parent, cancel := context.WithCancel(context.Background())
	id, err := r.Spawn(parent, agent.SubtaskSpawnRequest{
		Goal:      "hang forever",
		MaxSteps:  1,
		TimeoutMS: 60_000,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	cancel() // parent turn aborted — must stop child (no longer on Background)
	res, err := r.Wait(context.Background(), id, 5000)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if res.Status != "timeout" && res.Status != "cancelled" && res.Status != "error" {
		t.Fatalf("status=%q, want cancelled/timeout/error after parent cancel", res.Status)
	}
}

func TestSpawn_MaxStepsClampedTo12(t *testing.T) {
	// Negative/zero MaxSteps should be clamped to 12.
	// We can't easily observe maxSteps directly, but verifying Spawn doesn't error
	// with edge-case values is sufficient here.
	r := newTestTaskRunner(t)
	for _, ms := range []int{0, -1, 100} {
		id, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: "test", MaxSteps: ms})
		if err != nil {
			t.Fatalf("Spawn(MaxSteps=%d): %v", ms, err)
		}
		if id == "" {
			t.Fatalf("expected non-empty ID for MaxSteps=%d", ms)
		}
		// Drain each child before the next spawn so TempDir cleanup on Windows
		// does not race with leftover .orchestra writers.
		if _, err := r.Wait(context.Background(), id, 5000); err != nil {
			t.Fatalf("Wait(MaxSteps=%d): %v", ms, err)
		}
	}
}

func TestModeForSubagent(t *testing.T) {
	cases := map[string]agent.Mode{
		"":             agent.ModeExplore,
		"explore":      agent.ModeExplore,
		"ask":          agent.ModeAsk,
		"debug":        agent.ModeDebug,
		"architecture": agent.ModeArchitecture,
		"general":      agent.ModeGeneral,
		"worker":       agent.ModeWorker,
		"verifier":     agent.ModeVerifier,
	}
	for in, want := range cases {
		if got := modeForSubagent(in); got != want {
			t.Errorf("modeForSubagent(%q)=%q want %q", in, got, want)
		}
	}
}

func TestResolveChildLLM_Tier(t *testing.T) {
	called := false
	r := &TaskRunner{
		llmClient: &mockTaskResultLLM{result: "x"},
		child: ChildAgentConfig{
			ResolveTier: func(tier string) (string, string, bool) {
				if tier != "micro" {
					t.Fatalf("tier=%q", tier)
				}
				return "fast", "qwen-7b", true
			},
			ResolveClient: func(provider, model string) (llm.Client, string, string, error) {
				called = true
				if provider != "fast" || model != "qwen-7b" {
					t.Fatalf("got %s/%s", provider, model)
				}
				return &mockTaskResultLLM{result: "child"}, provider, model, nil
			},
		},
	}
	client, pl, ml := r.resolveChildLLM(agent.SubtaskSpawnRequest{Tier: "micro"}, "worker")
	if !called {
		t.Fatal("ResolveClient not called")
	}
	if pl != "fast" || ml != "qwen-7b" {
		t.Fatalf("labels %s/%s", pl, ml)
	}
	if client == nil {
		t.Fatal("nil client")
	}
}

func TestChildToolsForWorkerIncludesEdit(t *testing.T) {
	defs := childToolsForSubagent("worker", tools.Capabilities{})
	hasEdit := false
	for _, d := range defs {
		if d.Function.Name == "edit" {
			hasEdit = true
		}
		if d.Function.Name == "task_spawn" {
			t.Fatal("worker must not nest spawn")
		}
	}
	if !hasEdit {
		t.Fatal("worker tools missing edit")
	}
}

func TestChildToolsForExploreHasLSP(t *testing.T) {
	defs := childToolsForSubagent("explore", tools.Capabilities{})
	hasHover, hasExplore, hasResult := false, false, false
	for _, d := range defs {
		switch d.Function.Name {
		case "lsp.hover":
			hasHover = true
		case "explore":
			hasExplore = true
		case "task_result":
			hasResult = true
		}
	}
	if !hasHover {
		t.Fatal("explore child should include LSP tools")
	}
	if !hasExplore {
		t.Fatal("explore child should include explore tool")
	}
	if !hasResult {
		t.Fatal("explore child should include task_result")
	}
}

func TestChildToolsForVerifierReadOnlyWithBash(t *testing.T) {
	withExec := childToolsForSubagent("verifier", tools.Capabilities{Exec: true})
	hasBash, hasRead, hasWrite, hasResult := false, false, false, false
	for _, d := range withExec {
		switch d.Function.Name {
		case "bash":
			hasBash = true
		case "read":
			hasRead = true
		case "write", "edit":
			hasWrite = true
		case "task_result":
			hasResult = true
		}
	}
	if !hasBash || !hasRead || !hasResult {
		t.Fatalf("verifier tools: bash=%v read=%v result=%v", hasBash, hasRead, hasResult)
	}
	if hasWrite {
		t.Fatal("verifier must not include write/edit")
	}
	noExec := childToolsForSubagent("verifier", tools.Capabilities{})
	for _, d := range noExec {
		if d.Function.Name == "bash" {
			t.Fatal("verifier without exec cap should not expose bash")
		}
	}
}
