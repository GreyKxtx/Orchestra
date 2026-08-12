package tasks

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

func TestNormalizeEditPathSet(t *testing.T) {
	got := normalizeEditPathSet([]string{" ./a/b.go ", "a/c.go", "", "./a/b.go"})
	if len(got) != 2 {
		t.Fatalf("want 2 unique paths, got %v", got)
	}
	for _, want := range []string{"a/b.go", "a/c.go"} {
		if _, ok := got[want]; !ok {
			t.Fatalf("missing %q in %v", want, got)
		}
	}
	if normalizeEditPathSet(nil) != nil || normalizeEditPathSet([]string{" "}) != nil {
		t.Fatal("empty input must yield nil set")
	}
}

// gatedWorkerLLM counts Complete calls and blocks each one until release closes.
type gatedWorkerLLM struct {
	mu      sync.Mutex
	calls   int
	release chan struct{}
}

func (m *gatedWorkerLLM) Complete(ctx context.Context, _ llm.CompleteRequest) (*llm.CompleteResponse, error) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
	select {
	case <-m.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	input, _ := json.Marshal(map[string]string{"content": "done"})
	return &llm.CompleteResponse{
		Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: llm.ToolCallFunc{
					Name:      "task_result",
					Arguments: llm.ToolArguments(input),
				},
			}},
		},
	}, nil
}

func (m *gatedWorkerLLM) Plan(_ context.Context, _ string) (string, error) { return "", nil }

func (m *gatedWorkerLLM) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func waitForCalls(t *testing.T, m *gatedWorkerLLM, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if m.callCount() >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d LLM calls (got %d)", want, m.callCount())
}

// TestSpawn_SerializesOverlappingWorkOrders is the spec §5.6 disjoint check:
// overlapping target_files serialize, disjoint ones run in parallel.
func TestSpawn_SerializesOverlappingWorkOrders(t *testing.T) {
	mock := &gatedWorkerLLM{release: make(chan struct{})}
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("schema.NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(t.TempDir(), tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("tools.NewRunner: %v", err)
	}
	r := New(mock, v, tr, ChildAgentConfig{})
	t.Cleanup(func() {
		close(mock.release)
		r.Close()
		_ = tr.Close()
	})

	spawn := func(wo string) string {
		id, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{
			Goal: wo, SubagentType: "worker", MaxSteps: 2, TimeoutMS: 30_000,
		})
		if err != nil {
			t.Fatalf("Spawn(%s): %v", wo, err)
		}
		return id
	}

	// A starts and blocks inside its LLM call.
	spawn(`{"intent":"edit a","target_files":["pkg/a.go"]}`)
	waitForCalls(t, mock, 1)

	// B overlaps with A → must queue, no second LLM call.
	spawn(`{"intent":"edit a+b","target_files":["pkg/a.go","pkg/b.go"]}`)
	time.Sleep(150 * time.Millisecond)
	if got := mock.callCount(); got != 1 {
		t.Fatalf("overlapping WorkOrder must be serialized: %d LLM calls, want 1", got)
	}

	// C is disjoint → runs in parallel even while B is queued.
	spawn(`{"intent":"edit c","target_files":["pkg/c.go"]}`)
	waitForCalls(t, mock, 2)
	if got := mock.callCount(); got != 2 {
		t.Fatalf("disjoint WorkOrder must run in parallel: %d LLM calls, want 2", got)
	}
}
