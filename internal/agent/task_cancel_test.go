package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/protocol/schema"
)

// task_cancel is the last of the task_* group with no coverage, and the only
// one whose whole job is to make an earlier answer stop being true.
//
// The existing mockSubtaskRunner cancels by returning nil and forgetting, so
// nothing it is used in can tell a cancelled child from a finished one. This
// runner remembers, because that is the property worth checking: fs.delete
// answered "deleted" over a file that was still there, and a cancel that
// reports success while the child keeps running — or that lets a later wait
// answer "done" — is the same lie one level up. A Lead would then merge work
// it had decided to abandon.

// statefulRunner tracks each child so a wait after a cancel tells the truth.
type statefulRunner struct {
	mu        sync.Mutex
	n         int
	cancelled map[string]bool
	cancels   []string
}

func newStatefulRunner() *statefulRunner {
	return &statefulRunner{cancelled: map[string]bool{}}
}

func (s *statefulRunner) Spawn(_ context.Context, _ SubtaskSpawnRequest) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	return fmt.Sprintf("task_%d", s.n), nil
}

func (s *statefulRunner) Wait(_ context.Context, taskID string, _ int) (*SubtaskResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancelled[taskID] {
		return &SubtaskResult{TaskID: taskID, Status: "cancelled",
			Error: "cancelled: explicit task_cancel"}, nil
	}
	return &SubtaskResult{TaskID: taskID, Status: "done", Result: "the child changed greeting.go"}, nil
}

func (s *statefulRunner) Cancel(_ context.Context, taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if taskID != "task_1" && !strings.HasPrefix(taskID, "task_") {
		return fmt.Errorf("no such task %q", taskID)
	}
	s.cancelled[taskID] = true
	s.cancels = append(s.cancels, taskID)
	return nil
}

func agentWithRunner(t *testing.T, runner SubtaskRunner) *Agent {
	t.Helper()
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	tr, err := tools.NewRunner(t.TempDir(), tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { tr.Close() })
	ag, err := New(&todoScriptLLM{}, v, tr, Options{
		MaxSteps: 8, SubtaskRunner: runner,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return ag
}

func callTaskTool(t *testing.T, ag *Agent, name, args string) (string, error) {
	t.Helper()
	out, err := ag.handleTaskTool(context.Background(), name, "parent_call_1", json.RawMessage(args))
	if err != nil {
		return err.Error(), err
	}
	return string(out), nil
}

// The cancel has to reach the runner and be reported back as a cancellation,
// not as a generic success the model cannot distinguish from anything else.
func TestTaskCancel_ReachesTheRunnerAndSaysWhatItDid(t *testing.T) {
	runner := newStatefulRunner()
	ag := agentWithRunner(t, runner)

	id, err := callTaskTool(t, ag, "task_spawn", `{"goal":"change greeting.go","subagent_type":"worker"}`)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if !strings.Contains(id, "task_1") {
		t.Fatalf("spawn did not return a task id to cancel:\n%s", id)
	}

	out, err := callTaskTool(t, ag, "task_cancel", `{"task_id":"task_1"}`)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if len(runner.cancels) != 1 || runner.cancels[0] != "task_1" {
		t.Errorf("the cancel never reached the runner (got %v); the tool answered anyway",
			runner.cancels)
	}
	if !strings.Contains(out, "cancelled") {
		t.Errorf("the answer does not say the task was cancelled, so the model cannot tell "+
			"this from any other success:\n%s", out)
	}
	if !strings.Contains(out, "task_1") {
		t.Errorf("the answer does not name the task, so a Lead holding several cannot tell "+
			"which one stopped:\n%s", out)
	}
}

// The half that matters. A wait after a cancel must not report the child's
// work as done — that is how abandoned work gets merged.
func TestTaskCancel_AWaitAfterACancelDoesNotReportTheWorkAsDone(t *testing.T) {
	runner := newStatefulRunner()
	ag := agentWithRunner(t, runner)

	if _, err := callTaskTool(t, ag, "task_spawn", `{"goal":"change greeting.go","subagent_type":"worker"}`); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if _, err := callTaskTool(t, ag, "task_cancel", `{"task_id":"task_1"}`); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	out, err := callTaskTool(t, ag, "task_wait", `{"task_id":"task_1"}`)
	if err != nil {
		// An error here is an acceptable way to say "it was cancelled", as long
		// as it says so.
		if !strings.Contains(strings.ToLower(out), "cancel") {
			t.Fatalf("waiting on a cancelled task failed without saying it was cancelled: %s", out)
		}
		return
	}
	if strings.Contains(out, `"status":"done"`) {
		t.Errorf("a cancelled child reports done, and its result comes back as if it were "+
			"finished work:\n%s", out)
	}
	if !strings.Contains(out, "cancelled") {
		t.Errorf("the wait does not say the task was cancelled:\n%s", out)
	}
}

// A cancel with no id is the model mis-calling the tool, and the refusal has
// to name the missing argument — the alternative is a retry with the same
// mistake.
func TestTaskCancel_WithoutATaskIdSaysWhichArgumentIsMissing(t *testing.T) {
	ag := agentWithRunner(t, newStatefulRunner())

	out, err := callTaskTool(t, ag, "task_cancel", `{}`)
	if err == nil {
		t.Fatalf("a cancel naming no task was accepted:\n%s", out)
	}
	if !strings.Contains(out, "task_id") {
		t.Errorf("the refusal does not name the missing argument:\n%s", out)
	}
}

// A runner that refuses the cancel must not be reported as having done it.
func TestTaskCancel_AFailedCancelIsNotReportedAsSuccess(t *testing.T) {
	ag := agentWithRunner(t, &refusingRunner{})

	out, err := callTaskTool(t, ag, "task_cancel", `{"task_id":"task_9"}`)
	if err == nil {
		t.Fatalf("the runner refused the cancel and the tool answered success:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "cancel") {
		t.Errorf("the error does not say which operation failed:\n%s", out)
	}
}

type refusingRunner struct{ statefulRunner }

func (r *refusingRunner) Cancel(_ context.Context, taskID string) error {
	return fmt.Errorf("task %s already finished", taskID)
}
