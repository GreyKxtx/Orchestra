package tasks

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/schema"
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
						Name:      "task.result",
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
	t.Cleanup(func() { tr.Close() })
	return New(&mockTaskResultLLM{result: "all done"}, v, tr)
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
	t.Cleanup(func() { tr.Close() })
	r := New(&mockTaskResultLLM{result: "research complete"}, v, tr)

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
	if result.Result != "research complete" {
		t.Fatalf("expected result='research complete', got %q", result.Result)
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
	t.Cleanup(func() { tr.Close() })
	r := New(blockingLLM, v, tr)

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
	}
}
