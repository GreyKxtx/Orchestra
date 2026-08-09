package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
	"github.com/orchestra/orchestra/internal/tools"
)

// ChildClientResolver builds an LLM client for a child from optional provider/model overrides.
// Returns client plus labels for usage tracking. Nil resolver → always use TaskRunner.llmClient.
type ChildClientResolver func(provider, model string) (client llm.Client, providerLabel, modelLabel string, err error)

// TierResolver maps a worker tier name to provider/model (orchestra.tiers).
type TierResolver func(tier string) (provider, model string, ok bool)

// ChildAgentConfig holds history/memory settings propagated to child agents.
type ChildAgentConfig struct {
	MaxPromptBytes         int
	CompactThresholdPct    int
	ModelContextTokens     int
	CompletionMaxTokens    int
	ToolDigestBytes        int
	HistoryPruneKeepRecent int
	UsageTracker           agent.UsageRecorder
	ProviderLabel          string
	ModelLabel             string
	Caps                   tools.Capabilities
	ResolveClient          ChildClientResolver
	ResolveTier            TierResolver
	// MaxWorkerRetries caps validation/final failures for worker children (orchestra).
	MaxWorkerRetries int
	// LLMStepTimeout bounds each child LLM call. When 0, agent.Options defaults
	// to 25s — far too short for local/tunnelled models. Always set from
	// cfg.LLM.TimeoutS (see Core.buildChildAgentConfig).
	LLMStepTimeout time.Duration
	// MaxStepsCap clamps child MaxSteps (default 12). Parent may request less.
	MaxStepsCap int
	// OnChildEvent, when set, receives streaming events from child agents (E2E metrics).
	OnChildEvent func(agent.AgentEvent)
	// ChildEventSink builds a per-task OnEvent handler with child scope metadata.
	// Preferred over OnChildEvent in production core wiring.
	ChildEventSink func(taskID, parentToolCallID, subagentType string) func(agent.AgentEvent)
	// NotifyAgentEvent emits arbitrary agent/event payloads (child lifecycle).
	NotifyAgentEvent func(params map[string]any)
}

// TaskRunner implements agent.SubtaskRunner using real child agents.
// Child agents cannot spawn further subtasks (hasSubtasks=false).
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

func childToolsForSubagent(subagentType string, caps tools.Capabilities) []llm.ToolDef {
	var defs []llm.ToolDef
	switch strings.ToLower(strings.TrimSpace(subagentType)) {
	case "", "explore":
		defs = tools.ListToolsForMode("explore", caps, false, false)
	case "general":
		defs = tools.ListToolsForMode("general", caps, false, false)
	case "worker":
		defs = tools.ListToolsForMode("worker", caps, false, false)
	default:
		defs = tools.ListToolsForMode(subagentType, caps, false, false)
	}
	return ensureTaskResult(defs)
}

func ensureTaskResult(defs []llm.ToolDef) []llm.ToolDef {
	for _, d := range defs {
		if d.Function.Name == "task_result" {
			return defs
		}
	}
	return append(defs, tools.ToolTaskResult())
}

func modeForSubagent(subagentType string) agent.Mode {
	switch strings.ToLower(strings.TrimSpace(subagentType)) {
	case "", "explore":
		return agent.ModeExplore
	case "ask":
		return agent.ModeAsk
	case "debug":
		return agent.ModeDebug
	case "architecture":
		return agent.ModeArchitecture
	case "general":
		return agent.ModeGeneral
	case "worker":
		return agent.ModeWorker
	default:
		return agent.Mode(subagentType)
	}
}

// DefaultChildMaxSteps is the hard cap on child agent loop iterations when
// ChildAgentConfig.MaxStepsCap is unset.
const DefaultChildMaxSteps = 12

// DefaultTaskTimeoutMS is used for sync `task` and for `task_spawn` when the
// model omits timeout_ms (avoids orphan background children).
const DefaultTaskTimeoutMS = 120_000

// Spawn creates a new child agent task and starts it in a goroutine.
func (r *TaskRunner) Spawn(ctx context.Context, req agent.SubtaskSpawnRequest) (string, error) {
	r.mu.Lock()
	r.seq++
	taskID := fmt.Sprintf("task_%d_%d", r.seq, time.Now().UnixNano()%100000)
	r.mu.Unlock()

	capSteps := r.child.MaxStepsCap
	if capSteps <= 0 {
		capSteps = DefaultChildMaxSteps
	}
	maxSteps := req.MaxSteps
	if maxSteps <= 0 || maxSteps > capSteps {
		maxSteps = capSteps
	}

	subagentType := req.SubagentType
	if subagentType == "" {
		subagentType = "explore"
	}
	if strings.EqualFold(subagentType, "worker") {
		goal := strings.TrimSpace(req.Goal)
		if goal != "" && json.Valid([]byte(goal)) {
			if _, err := ParseWorkOrderJSON(goal); err != nil {
				return "", err
			}
		}
	}
	childTools := childToolsForSubagent(subagentType, r.child.Caps)

	// Inherit parent cancellation so finishing/cancelling the parent turn
	// stops orphaned children. Timeout still applies when TimeoutMS > 0.
	parent := ctx
	if parent == nil {
		parent = context.Background()
	}
	var taskCtx context.Context
	var cancel context.CancelFunc
	if req.TimeoutMS > 0 {
		taskCtx, cancel = context.WithTimeout(parent, time.Duration(req.TimeoutMS)*time.Millisecond)
	} else {
		taskCtx, cancel = context.WithCancel(parent)
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

		result := r.runChild(taskCtx, taskID, req, subagentType, maxSteps, childTools)

		r.mu.Lock()
		entry.result = result
		r.mu.Unlock()
	}()

	return taskID, nil
}

func (r *TaskRunner) resolveChildLLM(req agent.SubtaskSpawnRequest, subagentType string) (llm.Client, string, string) {
	provider := strings.TrimSpace(req.Provider)
	model := strings.TrimSpace(req.Model)
	if provider == "" && model == "" && strings.EqualFold(subagentType, "worker") && r.child.ResolveTier != nil {
		if p, m, ok := r.child.ResolveTier(req.Tier); ok {
			provider, model = p, m
		}
	}
	if r.child.ResolveClient != nil && (provider != "" || model != "") {
		if client, pl, ml, err := r.child.ResolveClient(provider, model); err == nil && client != nil {
			if pl == "" {
				pl = provider
			}
			if ml == "" {
				ml = model
			}
			return client, pl, ml
		}
	}
	pl := r.child.ProviderLabel
	ml := r.child.ModelLabel
	return r.llmClient, pl, ml
}

func (r *TaskRunner) runChild(ctx context.Context, taskID string, req agent.SubtaskSpawnRequest, subagentType string, maxSteps int, childTools []llm.ToolDef) *agent.SubtaskResult {
	if strings.EqualFold(subagentType, "worker") {
		goal := strings.TrimSpace(req.Goal)
		if goal != "" && json.Valid([]byte(goal)) {
			if _, err := ParseWorkOrderJSON(goal); err != nil {
				return &agent.SubtaskResult{TaskID: taskID, Status: "error", Error: err.Error()}
			}
		}
	}
	client, providerLabel, modelLabel := r.resolveChildLLM(req, subagentType)
	mode := modeForSubagent(subagentType)
	maxPrompt := r.child.MaxPromptBytes
	if maxPrompt <= 0 {
		maxPrompt = 64 * 1024
	}
	// Workers: tight budget — no parent dialog, only WorkOrder + tool reads.
	if mode == agent.ModeWorker && maxPrompt > 48*1024 {
		maxPrompt = 48 * 1024
	}
	opts := agent.Options{
		MaxSteps:               maxSteps,
		MaxPromptBytes:         maxPrompt,
		CompactThresholdPct:    r.child.CompactThresholdPct,
		ModelContextTokens:     r.child.ModelContextTokens,
		CompletionMaxTokens:    r.child.CompletionMaxTokens,
		ToolDigestBytes:        r.child.ToolDigestBytes,
		HistoryPruneKeepRecent: r.child.HistoryPruneKeepRecent,
		LLMStepTimeout:         r.child.LLMStepTimeout,
		CustomTools:            childTools,
		Mode:                   mode,
		IsChild:                true,
		UsageTracker:           r.child.UsageTracker,
		ProviderLabel:          providerLabel,
		ModelLabel:             modelLabel,
		// Workers: no parent dialog, no project memory inject, no session notes.
		AutoSessionMemory: false,
		SkipMemoryInject:  mode == agent.ModeWorker,
	}
	if mode == agent.ModeWorker {
		wsOff := false
		opts.WorkingState = &wsOff
		opts.TurnDigestKeep = 0
		opts.AssistantPrefill = "{"
	}
	if r.child.OnChildEvent != nil {
		opts.OnEvent = r.child.OnChildEvent
	}
	if r.child.ChildEventSink != nil {
		opts.OnEvent = r.child.ChildEventSink(taskID, req.ParentToolCallID, subagentType)
	}
	if r.child.NotifyAgentEvent != nil {
		r.child.NotifyAgentEvent(map[string]any{
			"type":                 "child_started",
			"task_id":              taskID,
			"parent_tool_call_id":  req.ParentToolCallID,
			"subagent_type":        subagentType,
			"content":              req.Goal,
		})
	}
	if mode == agent.ModeWorker && r.child.MaxWorkerRetries > 0 {
		opts.MaxFinalFailures = r.child.MaxWorkerRetries
		opts.MaxInvalidRetries = r.child.MaxWorkerRetries
		opts.MaxToolErrorRepeats = r.child.MaxWorkerRetries
	}
	ag, err := agent.New(client, r.validator, r.toolRunner, opts)
	if err != nil {
		return &agent.SubtaskResult{TaskID: taskID, Status: "error", Error: err.Error()}
	}

	childGoal := FormatChildGoal(subagentType, req.Tier, req.Goal)
	hist, res, runErr := ag.Run(ctx, nil, childGoal)
	status := "done"
	errMsg := ""
	if runErr != nil {
		if errors.Is(runErr, context.DeadlineExceeded) || errors.Is(runErr, context.Canceled) {
			status = "timeout"
		} else {
			status = "error"
		}
		errMsg = runErr.Error()
	}
	if r.child.NotifyAgentEvent != nil {
		params := map[string]any{
			"type":                "child_done",
			"task_id":             taskID,
			"parent_tool_call_id": req.ParentToolCallID,
			"subagent_type":       subagentType,
			"status":              status,
		}
		if errMsg != "" {
			params["error"] = errMsg
		}
		r.child.NotifyAgentEvent(params)
	}
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
		taskResult = agent.FormatSubagentResult(subagentType, req.Goal, hist, taskResult, r.child.ToolDigestBytes)
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
		// Wait for the child goroutine to exit before returning so callers
		// (and t.TempDir cleanup on Windows) do not race with late writes
		// under .orchestra/.
		<-entry.done
		r.mu.Lock()
		result := entry.result
		r.mu.Unlock()
		r.removeTask(taskID)
		if result != nil {
			return result, nil
		}
		if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
			return &agent.SubtaskResult{TaskID: taskID, Status: "timeout", Error: "wait timeout"}, nil
		}
		return &agent.SubtaskResult{TaskID: taskID, Status: "cancelled", Error: waitCtx.Err().Error()}, nil
	}
}

// Cancel aborts a running task.
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

// Close cancels every in-flight task and waits for its goroutine to exit.
// Call before releasing the shared tools.Runner / TempDir so Windows does not
// hit "directory is not empty" while a child is still writing under .orchestra/.
// Safe to call multiple times.
func (r *TaskRunner) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	entries := make([]*taskEntry, 0, len(r.tasks))
	for _, e := range r.tasks {
		entries = append(entries, e)
	}
	r.tasks = make(map[string]*taskEntry)
	r.mu.Unlock()
	for _, e := range entries {
		e.cancel()
	}
	for _, e := range entries {
		<-e.done
	}
}
