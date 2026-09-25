package tasks

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/llm"
)

// holdLLM answers each goal with "result of <goal>"; a goal containing HOLD
// waits for release first, so its task stays running.
type holdLLM struct {
	scriptLLM
	release chan struct{}
}

func newHoldLLM() *holdLLM {
	h := &holdLLM{release: make(chan struct{})}
	h.reply = func(req llm.CompleteRequest) llm.Message {
		text := conversation(req)
		for _, g := range []string{"A-ONE", "B-ONE", "A-TWO", "HOLD-L", "HOLD-C", "HOLD-A", "OTHER", "LATE"} {
			if strings.Contains(text, g) {
				return finish("result of " + g)
			}
		}
		return finish("?")
	}
	return h
}

func (h *holdLLM) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	if strings.Contains(conversation(req), "HOLD") {
		select {
		case <-h.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return h.scriptLLM.Complete(ctx, req)
}

// from is an agent that is itself task id, for spawning as it would.
func from(id string) agentScope {
	s := rootScope()
	s.taskID = id
	return s
}

func spawnAs(t *testing.T, r *TaskRunner, s agentScope, req agent.SubtaskSpawnRequest) (string, error) {
	t.Helper()
	if req.SubagentType == "" {
		req.SubagentType = "general"
	}
	req.TimeoutMS, req.MaxSteps = 30_000, 4
	return r.spawnFrom(context.Background(), s, req, spawnExtra{})
}

func waitStatus(t *testing.T, r *TaskRunner, id, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		got := r.findEntryLocked(id).status
		r.mu.Unlock()
		if got == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("task %s never reached %s", id, want)
}

// Two Leads both name their first WorkOrder wo-1. Keys used to be global: the
// second took the first's key over, and a dependent of either got the other
// Lead's upstream_results (ORC-7). Each Lead's keys are its own now.
func TestDependsOn_KeysAreTheSpawnersOwn(t *testing.T) {
	m := newHoldLLM()
	close(m.release)
	r, _ := newAgencyRunner(t, m, ChildAgentConfig{Agency: agencyOn()})

	a1, err := spawnAs(t, r, from("lead-A"), agent.SubtaskSpawnRequest{Goal: "A-ONE", Key: "wo-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Wait(context.Background(), a1, 10_000); err != nil {
		t.Fatal(err)
	}
	if _, err := spawnAs(t, r, from("lead-B"), agent.SubtaskSpawnRequest{Goal: "B-ONE", Key: "wo-1"}); err != nil {
		t.Fatal(err)
	}
	a2, err := spawnAs(t, r, from("lead-A"), agent.SubtaskSpawnRequest{Goal: "A-TWO", DependsOn: []string{"wo-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if res, _ := r.Wait(context.Background(), a2, 10_000); res == nil || res.Status != "done" {
		t.Fatalf("A-TWO: %+v", res)
	}
	got := conversation(m.requests("A-TWO")[0])
	if !strings.Contains(got, "result of A-ONE") || strings.Contains(got, "result of B-ONE") {
		t.Fatalf("A's dependent gets A's wo-1, not B's:\n%s", got)
	}

	if _, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: "x", SubagentType: "general", DependsOn: []string{"wo-1"}}); err == nil || !strings.Contains(err.Error(), "unknown task") {
		t.Fatalf("the root has no wo-1 of its own: %v", err)
	}
}

// A worker that depends on its own Lead waits for a task that waits for it:
// both hung until the 10-minute timeout. Any ancestor is refused at spawn.
func TestDependsOn_AnAncestorIsRefused(t *testing.T) {
	m := newHoldLLM()
	r, _ := newAgencyRunner(t, m, ChildAgentConfig{Agency: agencyOn()})
	t.Cleanup(func() { close(m.release) })

	lead, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: "HOLD-L", SubagentType: "general", Key: "lead", TimeoutMS: 30_000})
	if err != nil {
		t.Fatal(err)
	}
	child, err := spawnAs(t, r, from(lead), agent.SubtaskSpawnRequest{Goal: "HOLD-C"})
	if err != nil {
		t.Fatal(err)
	}
	for name, s := range map[string]agentScope{"its Lead": from(lead), "its grand-Lead": from(child)} {
		if _, err := spawnAs(t, r, s, agent.SubtaskSpawnRequest{Goal: "LATE", DependsOn: []string{lead}}); err == nil || !strings.Contains(err.Error(), "ancestor") {
			t.Errorf("a dependency on %s must be refused: %v", name, err)
		}
	}
}

// A task of another branch that has not started may be waiting for the very
// slot this task's Lead holds: with max_parallel 1, Lead A runs, Lead B queues
// behind it, and A's child waiting for B would wait for A. Refused until B has
// started; a finished task of another branch is fine.
func TestDependsOn_AnUnstartedTaskOfAnotherBranchIsRefused(t *testing.T) {
	m := newHoldLLM()
	settings := agencyOn()
	settings.MaxParallel = 1
	r, _ := newAgencyRunner(t, m, ChildAgentConfig{Agency: settings})
	released := false
	t.Cleanup(func() {
		if !released {
			close(m.release)
		}
	})

	leadA, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: "HOLD-A", SubagentType: "general", TimeoutMS: 30_000})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, r, leadA, "running")
	leadB, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: "OTHER", SubagentType: "general", TimeoutMS: 30_000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := spawnAs(t, r, from(leadA), agent.SubtaskSpawnRequest{Goal: "LATE", DependsOn: []string{leadB}}); err == nil || !strings.Contains(err.Error(), "has not started") {
		t.Fatalf("A's child must not wait on B queued behind A: %v", err)
	}

	close(m.release)
	released = true
	if res, _ := r.Wait(context.Background(), leadB, 10_000); res == nil || res.Status != "done" {
		t.Fatalf("B runs once A is done: %+v", res)
	}
	if _, err := spawnAs(t, r, from(leadA), agent.SubtaskSpawnRequest{Goal: "LATE", DependsOn: []string{leadB}}); err != nil {
		t.Fatalf("a finished task of another branch is a fine dependency: %v", err)
	}
}
