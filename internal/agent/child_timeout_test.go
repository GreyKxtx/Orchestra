package agent

import (
	"context"
	"encoding/json"
	"testing"
)

type timeoutRecordingRunner struct {
	spawned []SubtaskSpawnRequest
	waited  []int
}

func (r *timeoutRecordingRunner) Spawn(_ context.Context, req SubtaskSpawnRequest) (string, error) {
	r.spawned = append(r.spawned, req)
	return "task_1", nil
}

func (r *timeoutRecordingRunner) Wait(_ context.Context, _ string, timeoutMS int) (*SubtaskResult, error) {
	r.waited = append(r.waited, timeoutMS)
	return &SubtaskResult{TaskID: "task_1", Status: "done", Result: "ok"}, nil
}

func (r *timeoutRecordingRunner) Cancel(context.Context, string) error { return nil }

// A sync `task` without timeout_ms used to get 120 s — shorter than one
// worker on a local 27B model. The default now comes from the config
// (agent.child_timeout_s) and is ten minutes when unset; an explicit
// timeout_ms still wins.
func TestTask_DefaultChildTimeoutComesFromOptions(t *testing.T) {
	rec := &timeoutRecordingRunner{}
	a := &Agent{opts: Options{SubtaskRunner: rec}}
	if _, err := a.handleTaskTool(context.Background(), "task", "", json.RawMessage(`{"prompt":"look around"}`)); err != nil {
		t.Fatal(err)
	}
	if got := rec.spawned[0].TimeoutMS; got != DefaultChildTimeoutMS {
		t.Fatalf("unset config: lifetime = %d, want %d", got, DefaultChildTimeoutMS)
	}
	if got := rec.waited[0]; got != DefaultChildTimeoutMS {
		t.Fatalf("unset config: wait = %d, want %d", got, DefaultChildTimeoutMS)
	}

	rec = &timeoutRecordingRunner{}
	a = &Agent{opts: Options{SubtaskRunner: rec, ChildTimeoutMS: 900_000}}
	if _, err := a.handleTaskTool(context.Background(), "task_spawn", "", json.RawMessage(`{"goal":"look around"}`)); err != nil {
		t.Fatal(err)
	}
	if got := rec.spawned[0].TimeoutMS; got != 900_000 {
		t.Fatalf("configured: lifetime = %d, want 900000", got)
	}

	rec = &timeoutRecordingRunner{}
	a = &Agent{opts: Options{SubtaskRunner: rec, ChildTimeoutMS: 900_000}}
	if _, err := a.handleTaskTool(context.Background(), "task", "", json.RawMessage(`{"prompt":"quick","timeout_ms":30000}`)); err != nil {
		t.Fatal(err)
	}
	if got := rec.spawned[0].TimeoutMS; got != 30_000 {
		t.Fatalf("explicit timeout_ms must win, got %d", got)
	}
}
