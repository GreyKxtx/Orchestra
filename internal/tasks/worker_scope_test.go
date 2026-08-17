package tasks

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/protocol/schema"
)

func TestSpawn_OverlappingHandlerWaitsForLock(t *testing.T) {
	mock := &gatedWorkerLLM{release: make(chan struct{})}
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("schema.NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(t.TempDir(), tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("tools.NewRunner: %v", err)
	}
	verifyOff := false
	var mu sync.Mutex
	var events []map[string]any
	r := New(mock, v, tr, ChildAgentConfig{
		WorkerVerifyEnabled: &verifyOff,
		NotifyAgentEvent: func(params map[string]any) {
			cp := make(map[string]any, len(params))
			for k, val := range params {
				cp[k] = val
			}
			mu.Lock()
			events = append(events, cp)
			mu.Unlock()
		},
	})
	t.Cleanup(func() {
		r.Close()
		_ = tr.Close()
	})

	if _, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{
		Goal:         `{"intent":"edit A","target_files":["router.go","handler.go"]}`,
		SubagentType: "worker",
		MaxSteps:     2,
		TimeoutMS:    30_000,
	}); err != nil {
		t.Fatalf("Spawn A: %v", err)
	}
	waitForCalls(t, mock, 1)

	idB, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{
		Goal:         `{"intent":"edit B","target_files":["handler.go","service.go"]}`,
		SubagentType: "worker",
		MaxSteps:     2,
		TimeoutMS:    30_000,
	})
	if err != nil {
		t.Fatalf("Spawn B must not fail: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	queued := false
	for time.Now().Before(deadline) {
		mu.Lock()
		for _, ev := range events {
			if ev["type"] == "child_queued" && ev["task_id"] == idB {
				queued = true
			}
		}
		mu.Unlock()
		if queued {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !queued {
		t.Fatal("Worker B must emit child_queued while handler.go is locked")
	}
	if got := mock.callCount(); got != 1 {
		t.Fatalf("B must wait for A: %d LLM calls, want 1", got)
	}

	close(mock.release)

	resB, err := r.Wait(context.Background(), idB, 15_000)
	if err != nil {
		t.Fatalf("Wait B: %v", err)
	}
	if resB == nil {
		t.Fatal("Wait B returned nil result")
	}
	if resB.Status == "error" {
		t.Fatalf("Worker B must start after A, not fail: %+v", resB)
	}
	if mock.callCount() < 2 {
		t.Fatalf("Worker B must run after A: LLM calls=%d", mock.callCount())
	}
}
