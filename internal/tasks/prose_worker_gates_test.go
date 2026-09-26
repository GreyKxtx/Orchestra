package tasks

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/contract"
	"github.com/orchestra/orchestra/internal/tools"
)

// A worker whose goal is prose used to pass the contract and brief gates
// untouched: only a WorkOrder JSON was judged, so a Lead could spawn around
// them by not writing one (ORC-8). The gates judge a prose goal as an empty
// WorkOrder now.
func TestSpawn_AWorkerInProseMeetsTheGates(t *testing.T) {
	r := newTestTaskRunner(t)
	var seen []int
	r.child.GuardContractRefs = func(_ context.Context, refs []contract.Ref) error {
		seen = append(seen, len(refs))
		if len(refs) == 0 {
			return fmt.Errorf("runtime_guard: WorkOrder without contract_refs is invalid in execution once the contract is frozen")
		}
		return nil
	}
	_, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: "edit pkg/a.go so that Sum adds", SubagentType: "worker"})
	if err == nil || !strings.Contains(err.Error(), "contract_refs") {
		t.Fatalf("a worker in prose must meet the contract gate: %v", err)
	}
	if len(seen) != 1 || seen[0] != 0 {
		t.Fatalf("the gate must be asked once, with no refs: %v", seen)
	}
	// A scout is not a worker: no gate.
	seen = nil
	id, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: "look around", SubagentType: "explore"})
	if err != nil {
		t.Fatalf("explore: %v", err)
	}
	if _, err := r.Wait(context.Background(), id, 5000); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 0 {
		t.Fatalf("the contract gate is for workers: %v", seen)
	}
}

// The brief gate reads the department off the WorkOrder's scratchpad; a
// worker in prose for a department stands for its default one.
func TestSpawn_AWorkerInProseMeetsTheBriefGate(t *testing.T) {
	r, root := newAgencyRunner(t, &scriptedEdit{}, ChildAgentConfig{})
	writeFileT(t, root, ".orchestra/state.md", "---\norchestra:\n  phase: execution\n  prd_status: approved\n---\n")
	if _, err := r.toolRunner.FSWrite(context.Background(), tools.FSWriteRequest{
		Path: ".orchestra/playbooks/frontend.md", Content: "---\nbrief_required_fields:\n  - routes\n---\n\n## Rules\nx\n",
	}); err != nil {
		t.Fatal(err)
	}
	spawn := func() error {
		_, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{
			Goal: "add the routes to shared.go", SubagentType: "worker", Dept: "frontend", MaxSteps: 4, TimeoutMS: 30_000,
		})
		return err
	}
	if err := spawn(); err == nil || !strings.Contains(err.Error(), "brief_completeness") {
		t.Fatalf("a worker in prose for frontend must meet the brief gate: %v", err)
	}
	if _, err := r.toolRunner.FSWrite(context.Background(), tools.FSWriteRequest{
		Path: ".orchestra/specs/frontend/brief.md", Content: "# Brief\n\n## Routes\n/home\n",
	}); err != nil {
		t.Fatal(err)
	}
	if err := spawn(); err != nil {
		t.Fatalf("the brief satisfies the gate: %v", err)
	}
}
