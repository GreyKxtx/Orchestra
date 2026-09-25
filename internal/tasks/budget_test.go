package tasks

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/llm"
)

// gatedRunner's model blocks every call until release is called (at most
// once is enough; cleanup releases whatever the test did not).
func gatedRunner(t *testing.T, cfg ChildAgentConfig) (*TaskRunner, *gatedWorkerLLM, func()) {
	t.Helper()
	mock := &gatedWorkerLLM{release: make(chan struct{})}
	r, _ := newAgencyRunner(t, mock, cfg)
	var once sync.Once
	release := func() { once.Do(func() { close(mock.release) }) }
	t.Cleanup(release)
	return r, mock, release
}

func spawnGoal(t *testing.T, r *TaskRunner, goal string) string {
	t.Helper()
	id, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: goal, SubagentType: "general", MaxSteps: 2, TimeoutMS: 30_000})
	if err != nil {
		t.Fatalf("Spawn(%q): %v", goal, err)
	}
	return id
}

// task_wait's timeout bounds the wait, not the task (audit ORC-11): polling a
// long worker with a short timeout used to kill it.
func TestPoll_TimeoutLeavesTheTaskRunning(t *testing.T) {
	r, mock, release := gatedRunner(t, ChildAgentConfig{})
	id := spawnGoal(t, r, "long job")
	waitForCalls(t, mock, 1)

	res, err := r.Poll(context.Background(), id, 50)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "still_running" || !strings.Contains(res.Error, "keeps running") {
		t.Fatalf("a poll that times out must say the task still runs: %+v", res)
	}
	for _, e := range r.Board() {
		if e.TaskID == id && e.Finished {
			t.Fatalf("the poll cancelled the task: %+v", e)
		}
	}

	release()
	res, err = r.Poll(context.Background(), id, 30_000)
	if err != nil || res.Status != "done" {
		t.Fatalf("a second wait collects the finished task: %+v %v", res, err)
	}
	// Collected, and still on the board: waiting on it again is not "not found".
	again, err := r.Poll(context.Background(), id, 10)
	if err != nil || again.Status != "done" {
		t.Fatalf("a collected task answers with its result: %+v %v", again, err)
	}
}

// The synchronous task tool still gives up on a child it stops waiting for.
func TestWait_TimeoutStillCancelsTheTask(t *testing.T) {
	r, mock, _ := gatedRunner(t, ChildAgentConfig{})
	id := spawnGoal(t, r, "abandoned job")
	waitForCalls(t, mock, 1)
	res, err := r.Wait(context.Background(), id, 50)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "cancelled" && res.Status != "timeout" {
		t.Fatalf("Wait's timeout ends the task: %+v", res)
	}
}

// task_wait{task_ids}: one deadline for the set, and nothing it catches is cancelled.
func TestWaitMany_DeadlineReportsWithoutCancelling(t *testing.T) {
	quick := &scriptLLM{reply: func(req llm.CompleteRequest) llm.Message {
		if strings.Contains(conversation(req), "SLOW") {
			time.Sleep(400 * time.Millisecond)
		}
		return finish("ok")
	}}
	r, _ := newAgencyRunner(t, quick, ChildAgentConfig{Agency: agencyOn()})
	fast := spawnGoal(t, r, "FAST")
	slow := spawnGoal(t, r, "SLOW")
	if _, err := r.Wait(context.Background(), fast, 30_000); err != nil {
		t.Fatal(err)
	}
	res, err := r.WaitMany(context.Background(), []string{fast, slow}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if res.Results[0].Status != "done" || res.Results[1].Status != "still_running" {
		t.Fatalf("want done + still_running, got %+v %+v", res.Results[0], res.Results[1])
	}
	final, err := r.Poll(context.Background(), slow, 30_000)
	if err != nil || final.Status != "done" {
		t.Fatalf("the slow task was left to finish: %+v %v", final, err)
	}
}

func TestBudget_MaxTasks(t *testing.T) {
	r, _, _ := gatedRunner(t, ChildAgentConfig{Budget: TurnBudget{MaxTasks: 2}})
	spawnGoal(t, r, "one")
	spawnGoal(t, r, "two")
	_, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: "three", SubagentType: "general", MaxSteps: 2})
	if err == nil || !strings.Contains(err.Error(), "max_tasks") {
		t.Fatalf("the third task is over budget: %v", err)
	}
}

type fixedTokens int

func (f fixedTokens) Record(string, string, int, int)              {}
func (f fixedTokens) RecordCost(string, string, int, int, float64) {}
func (f fixedTokens) RecordCache(string, string, int, int)         {}
func (f fixedTokens) Total() (calls, prompt, completion, total int, costUSD float64) {
	return 1, int(f), 0, int(f), 0
}

func TestBudget_MaxTokens(t *testing.T) {
	r, _, _ := gatedRunner(t, ChildAgentConfig{Budget: TurnBudget{MaxTokens: 1000}, UsageTracker: fixedTokens(1500)})
	_, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: "one", SubagentType: "general", MaxSteps: 2})
	if err == nil || !strings.Contains(err.Error(), "max_tokens") {
		t.Fatalf("a turn past its token budget starts nothing new: %v", err)
	}
}

// When the tree's time runs out, what still runs is cancelled and nothing new starts.
func TestBudget_WallClockCancelsTheTree(t *testing.T) {
	r, mock, _ := gatedRunner(t, ChildAgentConfig{Budget: TurnBudget{MaxWall: 150 * time.Millisecond}})
	id := spawnGoal(t, r, "endless")
	waitForCalls(t, mock, 1)
	res, err := r.Poll(context.Background(), id, 30_000)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "cancelled" || !strings.Contains(res.Error, "time budget") {
		t.Fatalf("the budget must cancel the running task and say why: %+v", res)
	}
	_, err = r.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: "more", SubagentType: "general", MaxSteps: 2})
	if err == nil || !strings.Contains(err.Error(), "max_wall_s") {
		t.Fatalf("no new task after the budget ran out: %v", err)
	}
}

// The same task twice at once is a loop, not parallelism.
func TestAdmit_IdenticalTaskWhileRunning(t *testing.T) {
	r, _, _ := gatedRunner(t, ChildAgentConfig{})
	first := spawnGoal(t, r, "Fix  the\nparser")
	_, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: "fix the parser", SubagentType: "general", MaxSteps: 2})
	if err != nil {
		t.Fatalf("a different goal (case differs) is a different task: %v", err)
	}
	_, err = r.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: "Fix the parser", SubagentType: "general", MaxSteps: 2})
	if err == nil || !strings.Contains(err.Error(), first) {
		t.Fatalf("an identical task (whitespace aside) is refused while %s runs: %v", first, err)
	}
}

// A task that failed twice is not started a third time.
func TestAdmit_IdenticalTaskAfterTwoFailures(t *testing.T) {
	r, _ := newAgencyRunner(t, erroringLLM{}, ChildAgentConfig{})
	wo := `{"task_id":"wo-1","intent":"fix it","target_files":["a.go"]}`
	woReordered := `{"target_files":["a.go"],  "intent":"fix it", "task_id":"wo-1"}`
	for i, goal := range []string{wo, woReordered} {
		res := spawnAndWait(t, r, agent.SubtaskSpawnRequest{Goal: goal, SubagentType: "general", MaxSteps: 1})
		if res.Status == "done" {
			t.Fatalf("attempt %d was meant to fail: %+v", i+1, res)
		}
	}
	_, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: wo, SubagentType: "general", MaxSteps: 1})
	if err == nil || !strings.Contains(err.Error(), "failed 2 times") {
		t.Fatalf("the third identical attempt (key order aside) must be refused: %v", err)
	}
	if _, err := r.Spawn(context.Background(), agent.SubtaskSpawnRequest{Goal: "a different approach", SubagentType: "general", MaxSteps: 1}); err != nil {
		t.Fatalf("a different task is still allowed: %v", err)
	}
}

// erroringLLM fails every call, so every child it runs ends in error.
type erroringLLM struct{}

func (erroringLLM) Complete(context.Context, llm.CompleteRequest) (*llm.CompleteResponse, error) {
	return nil, errors.New("provider down")
}
func (erroringLLM) Plan(context.Context, string) (string, error) { return "", nil }

func TestTaskFingerprint(t *testing.T) {
	if taskFingerprint("general", `{"a":1,"b":2}`) != taskFingerprint("GENERAL", `{ "b": 2, "a": 1 }`) {
		t.Error("a WorkOrder is compared by content")
	}
	if taskFingerprint("general", "x") == taskFingerprint("explore", "x") {
		t.Error("the same goal for another agent is another task")
	}
}
