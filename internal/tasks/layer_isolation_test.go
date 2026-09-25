package tasks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/llm"
)

// scriptedEdit is a worker that reads shared.go, edits it and reports. The
// first task it serves (told apart by the call's trace, since a failed
// worker's lesson reaches the next one's prompt) fails; the rest succeed.
type scriptedEdit struct {
	mu    sync.Mutex
	first string
}

func (s *scriptedEdit) Plan(context.Context, string) (string, error) { return "{}", nil }

func (s *scriptedEdit) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	task := llm.TraceFrom(ctx).TaskID
	s.mu.Lock()
	if s.first == "" {
		s.first = task
	}
	fail := task == s.first
	s.mu.Unlock()
	marker, report := "first", `{"status":"success","path":"shared.go","summary":"V changed"}`
	if fail {
		marker, report = "broken", `{"status":"error","reason":"could not finish"}`
	}
	steps := 0
	for _, m := range req.Messages {
		if m.Role == llm.RoleAssistant {
			steps++
		}
	}
	switch steps {
	case 0:
		return &llm.CompleteResponse{Message: callTool("read", map[string]string{"path": "shared.go"})}, nil
	case 1:
		return &llm.CompleteResponse{Message: callTool("edit", map[string]string{
			"path": "shared.go", "search": `const V = "disk"`, "replace": `const V = "` + marker + `"`,
		})}, nil
	default:
		return &llm.CompleteResponse{Message: finish(report)}, nil
	}
}

func sharedFile(t *testing.T, root string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "shared.go"), []byte("package shared\n\nconst V = \"disk\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func stagedShared(t *testing.T, r *TaskRunner) string {
	t.Helper()
	for p, c := range r.toolRunner.StagedFileContent(context.Background()) {
		if p == "shared.go" {
			return c
		}
	}
	return ""
}

// A worker that fails leaves nothing behind: its edits never reach the turn
// that would apply them (ORC-1). They used to stay staged in the one overlay
// every agent shared, and the turn's final apply wrote them.
func TestLayer_FailedWorkerEditsDoNotReachTheTurn(t *testing.T) {
	r, root := newAgencyRunner(t, &scriptedEdit{}, ChildAgentConfig{})
	sharedFile(t, root)
	res := spawnAndWait(t, r, agent.SubtaskSpawnRequest{
		Goal:         `{"task_id":"wo-1","intent":"change V","target_files":["shared.go"]}`,
		SubagentType: "worker",
	})
	if strings.Contains(res.Result, "verified_success") {
		t.Fatalf("the worker was meant to fail: %+v", res)
	}
	if got := stagedShared(t, r); got != "" {
		t.Fatalf("a failed worker's edit reached the turn:\n%s", got)
	}

	ok := spawnAndWait(t, r, agent.SubtaskSpawnRequest{
		Goal:         `{"task_id":"wo-2","intent":"change V","target_files":["shared.go"]}`,
		SubagentType: "worker",
	})
	if !strings.Contains(stagedShared(t, r), `"first"`) {
		t.Fatalf("a verified worker's edit reaches the turn: %+v\n%q", ok, stagedShared(t, r))
	}
}

// Two tasks rewrite one file from the same version at once. The first to
// finish lands; the second is a merge conflict and changes nothing — not a
// silent overwrite of the first.
func TestLayer_TwoTasksOnOneFileConflict(t *testing.T) {
	var arrive sync.WaitGroup
	arrive.Add(2)
	proceed := make(chan struct{})
	mock := &twoWriters{arrive: &arrive, proceed: proceed}
	r, root := newAgencyRunner(t, mock, ChildAgentConfig{})
	sharedFile(t, root)

	first, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: "FIRST rewrite shared.go", SubagentType: "general", MaxSteps: 6, TimeoutMS: 30_000})
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: "SECOND rewrite shared.go", SubagentType: "general", MaxSteps: 6, TimeoutMS: 30_000})
	if err != nil {
		t.Fatal(err)
	}
	arrive.Wait() // both have written, neither has committed
	close(proceed)
	r1, _ := r.Wait(context.Background(), first, 30_000)
	r2, _ := r.Wait(context.Background(), second, 30_000)

	var won, lost *agent.SubtaskResult
	switch {
	case r1.Status == "done" && r2.Status != "done":
		won, lost = r1, r2
	case r2.Status == "done" && r1.Status != "done":
		won, lost = r2, r1
	default:
		t.Fatalf("exactly one task must land: %+v / %+v", r1, r2)
	}
	if !strings.Contains(lost.Error, "merge_conflict") || !strings.Contains(lost.Error, "shared.go") {
		t.Errorf("the second task is a merge conflict naming the file: %+v", lost)
	}
	winner := "first"
	if won == r2 {
		winner = "second"
	}
	if got := stagedShared(t, r); !strings.Contains(got, `"`+winner+`"`) {
		t.Errorf("the file holds the winner's version, not a mix or the loser's:\n%s", got)
	}
}

// twoWriters is scriptedEdit for general children that both rewrite
// shared.go and wait for each other before reporting.
type twoWriters struct {
	mu      sync.Mutex
	arrive  *sync.WaitGroup
	proceed chan struct{}
	waited  map[string]bool
}

func (w *twoWriters) Plan(context.Context, string) (string, error) { return "{}", nil }

func (w *twoWriters) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	goal := conversation(req)
	marker := "first"
	if strings.Contains(goal, "SECOND") {
		marker = "second"
	}
	steps := 0
	for _, m := range req.Messages {
		if m.Role == llm.RoleAssistant {
			steps++
		}
	}
	switch steps {
	case 0:
		return &llm.CompleteResponse{Message: callTool("read", map[string]string{"path": "shared.go"})}, nil
	case 1:
		return &llm.CompleteResponse{Message: callTool("edit", map[string]string{
			"path": "shared.go", "search": `const V = "disk"`, "replace": `const V = "` + marker + `"`,
		})}, nil
	default:
		w.mu.Lock()
		if w.waited == nil {
			w.waited = map[string]bool{}
		}
		first := !w.waited[marker]
		w.waited[marker] = true
		w.mu.Unlock()
		if first {
			w.arrive.Done()
			<-w.proceed
		}
		return &llm.CompleteResponse{Message: finish("rewrote shared.go: " + marker)}, nil
	}
}
