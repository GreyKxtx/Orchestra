package tasks

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/schema"
	"github.com/orchestra/orchestra/internal/tools"
)

// ChildAgentConfig holds history/memory settings propagated to child agents.
type ChildAgentConfig struct {
	MaxPromptBytes         int
	CompactThresholdPct    int
	ToolDigestBytes        int
	HistoryPruneKeepRecent int
	UsageTracker           agent.UsageRecorder
	ProviderLabel          string
	ModelLabel             string
}

// TaskRunner implements agent.SubtaskRunner using real child agents.
// Child agents run with a read-only tool set and cannot spawn further subtasks.
type TaskRunner struct {
	llmClient  llm.Client
	validator  *schema.Validator
	toolRunner *tools.Runner
	child      ChildAgentConfig

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
func New(llmClient llm.Client, validator *schema.Validator, toolRunner *tools.Runner, child ChildAgentConfig) *TaskRunner {
	return &TaskRunner{
		llmClient:  llmClient,
		validator:  validator,
		toolRunner: toolRunner,
		child:      child,
		tasks:      make(map[string]*taskEntry),
	}
}

func childToolsForSubagent(subagentType string) []llm.ToolDef {
	switch subagentType {
	case "", "explore":
		return tools.ListToolsForChild()
	case "general":
		return tools.ListToolsForMode("general", tools.Capabilities{}, false, false)
	default:
		return tools.ListToolsForMode(subagentType, tools.Capabilities{}, false, false)
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

	subagentType := req.SubagentType
	if subagentType == "" {
		subagentType = "explore"
	}
	childTools := childToolsForSubagent(subagentType)

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

	go func() {
		defer close(entry.done)
		defer cancel()

		result := r.runChild(taskCtx, taskID, req.Goal, subagentType, maxSteps, childTools)

		r.mu.Lock()
		entry.result = result
		r.mu.Unlock()
	}()

	return taskID, nil
}

func (r *TaskRunner) runChild(ctx context.Context, taskID, goal, subagentType string, maxSteps int, childTools []llm.ToolDef) *agent.SubtaskResult {
	maxPrompt := r.child.MaxPromptBytes
	if maxPrompt <= 0 {
		maxPrompt = 64 * 1024
	}
	ag, err := agent.New(r.llmClient, r.validator, r.toolRunner, agent.Options{
		MaxSteps:               maxSteps,
		MaxPromptBytes:         maxPrompt,
		CompactThresholdPct:    r.child.CompactThresholdPct,
		ToolDigestBytes:        r.child.ToolDigestBytes,
		HistoryPruneKeepRecent: r.child.HistoryPruneKeepRecent,
		CustomTools:            childTools,
		IsChild:                true,
		UsageTracker:           r.child.UsageTracker,
		ProviderLabel:          r.child.ProviderLabel,
		ModelLabel:             r.child.ModelLabel,
	})
	if err != nil {
		return &agent.SubtaskResult{TaskID: taskID, Status: "error", Error: err.Error()}
	}

	hist, res, runErr := ag.Run(ctx, nil, goal)
	if runErr != nil {
		if errors.Is(runErr, context.DeadlineExceeded) || errors.Is(runErr, context.Canceled) {
			return &agent.SubtaskResult{TaskID: taskID, Status: "timeout", Error: runErr.Error()}
		}
		return &agent.SubtaskResult{TaskID: taskID, Status: "error", Error: runErr.Error()}
	}

	taskResult := ""
	if res != nil {
		taskResult = res.SubtaskResult
		if taskResult == "" && len(res.Patches) > 0 {
			taskResult = fmt.Sprintf("completed with %d patch(es)", len(res.Patches))
		}
	}

	if subagentType == "" || subagentType == "explore" {
		taskResult = agent.FormatSubagentResult(subagentType, goal, hist, taskResult, r.child.ToolDigestBytes)
	}

	return &agent.SubtaskResult{TaskID: taskID, Status: "done", Result: taskResult}
}

func (r *TaskRunner) removeTask(taskID string) {
	r.mu.Lock()
	delete(r.tasks, taskID)
	r.mu.Unlock()
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
		r.removeTask(taskID)
		if result == nil {
			return &agent.SubtaskResult{TaskID: taskID, Status: "error", Error: "task produced no result"}, nil
		}
		return result, nil
	case <-waitCtx.Done():
		entry.cancel()
		r.removeTask(taskID)
		if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
			return &agent.SubtaskResult{TaskID: taskID, Status: "timeout", Error: "wait timeout"}, nil
		}
		return &agent.SubtaskResult{TaskID: taskID, Status: "cancelled", Error: waitCtx.Err().Error()}, nil
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
