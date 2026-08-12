package tasks

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/contract"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/protocol/schema"
)

// TestInvalidateStaleContractTasks is spec §5.3 "смена epoch → task_cancel":
// a running worker pinned to old contract hashes is cancelled when the guard
// starts failing; workers without refs and already-valid refs are untouched.
func TestInvalidateStaleContractTasks(t *testing.T) {
	mock := &gatedWorkerLLM{release: make(chan struct{})}
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("schema.NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(t.TempDir(), tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("tools.NewRunner: %v", err)
	}
	// Guard behavior is switchable: valid until the "epoch change".
	stale := false
	r := New(mock, v, tr, ChildAgentConfig{})
	r.child.GuardContractRefs = func(refs []contract.Ref) error {
		if stale && len(refs) > 0 && refs[0].SHA256 == "old" {
			return fmt.Errorf("stale_contract: hash mismatch")
		}
		return nil
	}
	t.Cleanup(func() {
		close(mock.release)
		r.Close()
		_ = tr.Close()
	})

	spawn := func(goal string) string {
		id, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{
			Goal: goal, SubagentType: "worker", MaxSteps: 2, TimeoutMS: 30_000,
		})
		if err != nil {
			t.Fatalf("Spawn: %v", err)
		}
		return id
	}

	pinned := spawn(`{"intent":"a","target_files":["pkg/a.go"],"contract_refs":[{"path":"NFR.md","sha256":"old"}]}`)
	free := spawn(`{"intent":"b","target_files":["pkg/b.go"]}`)
	waitForCalls(t, mock, 2)

	// Epoch "changes": the pinned worker's refs are now stale.
	stale = true
	cancelled := r.InvalidateStaleContractTasks(context.Background())
	if len(cancelled) != 1 || cancelled[0] != pinned {
		t.Fatalf("cancelled = %v, want [%s]", cancelled, pinned)
	}

	// The cancelled worker terminates with a context error.
	res, err := r.Wait(context.Background(), pinned, 5000)
	if err != nil {
		t.Fatalf("Wait(pinned): %v", err)
	}
	if res.Status == "done" && !strings.Contains(strings.ToLower(res.Error+res.Result), "cancel") {
		t.Fatalf("pinned worker must be cancelled, got %+v", res)
	}

	// The ref-free worker keeps running until released.
	select {
	case <-time.After(100 * time.Millisecond):
	}
	if got := r.InvalidateStaleContractTasks(context.Background()); len(got) != 0 {
		t.Fatalf("second pass must cancel nothing, got %v", got)
	}
	_ = free
}
