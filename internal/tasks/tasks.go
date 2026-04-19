package tasks

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/schema"
	"github.com/orchestra/orchestra/internal/tools"
)

// TaskRunner implements agent.SubtaskRunner using real child agents.
// Child agents run with a read-only tool set and cannot spawn further subtasks.
type TaskRunner struct {
	llmClient  llm.Client
	validator  *schema.Validator
	toolRunner *tools.Runner

	mu    sync.Mutex
	tasks map[string]*taskEntry
	seq   int
}

type taskEntry struct {
	id     string
	cancel context.CancelFunc
	done   chan struct{}
	result *agent.SubtaskResult
}

// New creates a new TaskRunner.
func New(llmClient llm.Client, validator *schema.Validator, toolRunner *tools.Runner) *TaskRunner {
	return &TaskRunner{
		llmClient:  llmClient,
		validator:  validator,
		toolRunner: toolRunner,
		tasks:      make(map[string]*taskEntry),
	}
}

// Spawn creates a new child agent task and starts it in a goroutine.
func (r *TaskRunner) Spawn(_ context.Context, req agent.SubtaskSpawnRequest) (string, error) {
	r.mu.Lock()
	r.seq++
	taskID := fmt.Sprintf("task_%d_%d", r.seq, time.Now().UnixNano()%100000)
	r.mu.Unlock()

	maxSteps := req.MaxSteps
	if maxSteps <= 0 || maxSteps > 12 {
		maxSteps = 12
	}

	var taskCtx context.Context
	var cancel context.CancelFunc
	if req.TimeoutMS > 0 {
		taskCtx, cancel = context.WithTimeout(context.Background(), time.Duration(req.TimeoutMS)*time.Millisecond)
	} else {
		taskCtx, cancel = context.WithCancel(context.Background())
	}

	entry := &taskEntry{
		id:     taskID,
		cancel: cancel,
		done:   make(chan struct{}),
	}

	r.mu.Lock()
	r.tasks[taskID] = entry
	r.mu.Unlock()

	childTools := tools.ListToolsForChild()
	go func() {
		defer close(entry.done)
		defer cancel()

		result := r.runChild(taskCtx, taskID, req.Goal, maxSteps, childTools)

		r.mu.Lock()
		entry.result = result
		r.mu.Unlock()
	}()

	return taskID, nil
}

func (r *TaskRunner) runChild(ctx context.Context, taskID, goal string, maxSteps int, childTools []llm.ToolDef) *agent.SubtaskResult {
	ag, err := agent.New(r.llmClient, r.validator, r.toolRunner, agent.Options{
		MaxSteps:    maxSteps,
		CustomTools: childTools,
		// SubtaskRunner intentionally nil — prevents recursive spawning
	})
	if err != nil {
		return &agent.SubtaskResult{TaskID: taskID, Status: "error", Error: err.Error()}
	}

	_, res, runErr := ag.Run(ctx, nil, goal)
	if runErr != nil {
		return &agent.SubtaskResult{TaskID: taskID, Status: "error", Error: runErr.Error()}
	}
	if res.SubtaskResult != "" {
		return &agent.SubtaskResult{TaskID: taskID, Status: "done", Result: res.SubtaskResult}
	}
	// Child finished with patches (unusual for research tasks — summarize)
	return &agent.SubtaskResult{
		TaskID: taskID,
		Status: "done",
		Result: fmt.Sprintf("completed with %d patch(es)", len(res.Patches)),
	}
}

// Wait blocks until the task completes, or the timeout/ctx expires.
func (r *TaskRunner) Wait(ctx context.Context, taskID string, timeoutMS int) (*agent.SubtaskResult, error) {
	r.mu.Lock()
	entry, ok := r.tasks[taskID]
	r.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("task %q not found", taskID)
	}

	waitCtx := ctx
	var cancel context.CancelFunc
	if timeoutMS > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
		defer cancel()
	}

	select {
	case <-entry.done:
		r.mu.Lock()
		result := entry.result
		r.mu.Unlock()
		if result == nil {
			return &agent.SubtaskResult{TaskID: taskID, Status: "error", Error: "task produced no result"}, nil
		}
		return result, nil
	case <-waitCtx.Done():
		return &agent.SubtaskResult{TaskID: taskID, Status: "cancelled", Error: "wait timeout"}, nil
	}
}

// Cancel cancels a running task.
func (r *TaskRunner) Cancel(_ context.Context, taskID string) error {
	r.mu.Lock()
	entry, ok := r.tasks[taskID]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("task %q not found", taskID)
	}
	entry.cancel()
	return nil
}
