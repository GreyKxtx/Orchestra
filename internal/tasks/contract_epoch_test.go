package tasks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/contract"
	"github.com/orchestra/orchestra/internal/wsview"
	"github.com/orchestra/orchestra/llm"
)

const nfrFrozen = "## Latency\np95 < 200ms\n\n## Availability\n99.9%\n"

// epochScript plays two agents. The worker reads svc.go, edits it and then
// waits — for its context to end, or to be released. The owner, a general
// child, changes NFR.md and reports.
type epochScript struct {
	blocked chan struct{} // closed once the worker has edited and waits
	release chan struct{}
	once    sync.Once
}

func (s *epochScript) Plan(context.Context, string) (string, error) { return "{}", nil }

func (s *epochScript) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	steps := 0
	for _, m := range req.Messages {
		if m.Role == llm.RoleAssistant {
			steps++
		}
	}
	if strings.Contains(conversation(req), "OWNER") {
		switch steps {
		case 0:
			return &llm.CompleteResponse{Message: callTool("read", map[string]string{"path": ".orchestra/contract/NFR.md"})}, nil
		case 1:
			return &llm.CompleteResponse{Message: callTool("edit", map[string]string{
				"path": ".orchestra/contract/NFR.md", "search": "p95 < 200ms", "replace": "p95 < 50ms",
			})}, nil
		default:
			return &llm.CompleteResponse{Message: finish("tightened the latency budget")}, nil
		}
	}
	switch steps {
	case 0:
		return &llm.CompleteResponse{Message: callTool("read", map[string]string{"path": "svc.go"})}, nil
	case 1:
		return &llm.CompleteResponse{Message: callTool("edit", map[string]string{
			"path": "svc.go", "search": "const Budget = 200", "replace": "const Budget = 199",
		})}, nil
	default:
		s.once.Do(func() { close(s.blocked) })
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.release:
		}
		return &llm.CompleteResponse{Message: finish(`{"status":"success","path":"svc.go","summary":"budget"}`)}, nil
	}
}

// frozenContract writes NFR.md and svc.go to disk and freezes NFR.md.
func frozenContract(t *testing.T, root string) []contract.Ref {
	t.Helper()
	nfr := filepath.Join(root, ".orchestra", "contract", "NFR.md")
	if err := os.MkdirAll(filepath.Dir(nfr), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nfr, []byte(nfrFrozen), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "svc.go"), []byte("package svc\n\nconst Budget = 200\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e, err := contract.UpdateArtifact(root, wsview.Disk(root), contract.ArtifactNFR, contract.OwnerOrchestrator)
	if err != nil {
		t.Fatal(err)
	}
	return []contract.Ref{{Path: contract.DirRel + "/" + contract.ArtifactNFR, SHA256: e.Artifacts[contract.ArtifactNFR].SHA256}}
}

// An owner's change to a frozen artifact is an epoch change the moment its
// task commits: the runtime records it in EPOCH.yaml and cancels the running
// worker whose WorkOrder was written against the old version. The worker's
// edits go with it — nothing of them reaches the turn. The epoch hook used to
// fire only for a Lead writing straight to disk, which no role could do, so
// in orchestra mode this never happened (ORC-5).
func TestEpochChange_CancelsStaleWorkersAndDropsTheirLayers(t *testing.T) {
	script := &epochScript{blocked: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(func() { close(script.release) })
	r, root := newAgencyRunner(t, script, ChildAgentConfig{})
	r.child.GuardContractRefs = func(ctx context.Context, refs []contract.Ref) error {
		return contract.VerifyRefs(root, r.toolRunner.View(ctx), refs)
	}
	refs := frozenContract(t, root)

	wo, _ := json.Marshal(map[string]any{
		"task_id": "wo-1", "intent": "tune the budget", "target_files": []string{"svc.go"}, "contract_refs": refs,
	})
	worker, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{
		Goal: string(wo), SubagentType: "worker", MaxSteps: 6, TimeoutMS: 30_000,
	})
	if err != nil {
		t.Fatalf("a WorkOrder on the frozen contract spawns: %v", err)
	}
	select {
	case <-script.blocked:
	case <-time.After(10 * time.Second):
		t.Fatal("the worker never reached its edit")
	}

	owner := spawnAndWait(t, r, agent.SubtaskSpawnRequest{Goal: "OWNER: tighten NFR.md", SubagentType: "general"})
	if owner.Status != "done" {
		t.Fatalf("the owner's change lands: %+v", owner)
	}

	res, err := r.Wait(context.Background(), worker, 10_000)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "cancelled" || !strings.Contains(res.Error, "contract") {
		t.Fatalf("the stale worker is cancelled for its contract: %+v", res)
	}
	staged := r.toolRunner.StagedFileContent(context.Background())
	if _, ok := staged["svc.go"]; ok {
		t.Fatalf("the cancelled worker's edit reached the turn:\n%s", staged["svc.go"])
	}
	if !strings.Contains(staged[".orchestra/contract/NFR.md"], "p95 < 50ms") {
		t.Fatalf("the owner's change is the turn's: %v", staged)
	}
	e, _, err := contract.Load(root)
	if err != nil || e.Epoch != 2 || e.Artifacts[contract.ArtifactNFR].Version != 2 {
		t.Fatalf("EPOCH.yaml records the change: %+v %v", e, err)
	}

	// A WorkOrder still carrying the old hash is refused from now on.
	if _, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{
		Goal: string(wo), SubagentType: "worker", MaxSteps: 6, TimeoutMS: 30_000,
	}); err == nil || !strings.Contains(err.Error(), "stale_contract") {
		t.Fatalf("an old WorkOrder must be regenerated: %v", err)
	}
}
