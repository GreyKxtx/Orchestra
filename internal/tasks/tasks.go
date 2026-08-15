package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/contract"
	"github.com/orchestra/orchestra/internal/orchestrastate"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
	"github.com/orchestra/orchestra/internal/tools"
)

// ChildClientResolver builds an LLM client for a child from optional provider/model overrides.
// Returns client plus labels for usage tracking. Nil resolver → always use TaskRunner.llmClient.
type ChildClientResolver func(provider, model string) (client llm.Client, providerLabel, modelLabel string, err error)

// TierResolver maps a worker tier name to provider/model (orchestra.tiers
// or orchestra_routing.yaml roles).
type TierResolver func(tier string) (provider, model string, ok bool)

// TaskTypeRoute is the routing decision for a task_type (orchestra_routing.yaml).
type TaskTypeRoute struct {
	SubagentType string
	Tier         string // legacy worker band (complex|focused|micro)
	Provider     string
	Model        string
}

// TaskTypeRouter resolves a task_type into default spawn parameters.
type TaskTypeRouter func(taskType string) (TaskTypeRoute, bool)

// TierEscalationSettings mirrors orchestra.tier_escalation (spec §5.5):
// after FailuresBeforeEscalation failed verification rounds on the assigned
// tier, the same WorkOrder is re-run on EscalationTier; when that fails too
// the result stays verification_failed for the Lead to replan.
type TierEscalationSettings struct {
	Enabled                  bool
	FailuresBeforeEscalation int    // base-tier attempts (default 2)
	MaxEscalatedRetries      int    // escalated-tier attempts (default 1)
	EscalationTier           string // tier name resolved via TierResolver (default "complex")
}

func (t TierEscalationSettings) baseRounds() int {
	if t.FailuresBeforeEscalation <= 0 {
		return 2
	}
	return t.FailuresBeforeEscalation
}

func (t TierEscalationSettings) escalatedRounds() int {
	if t.MaxEscalatedRetries <= 0 {
		return 1
	}
	return t.MaxEscalatedRetries
}

func (t TierEscalationSettings) tierName() string {
	if v := strings.TrimSpace(t.EscalationTier); v != "" {
		return v
	}
	return "complex"
}

// SpawnGuard is the fail-closed phase gate evaluated before a child starts
// (orchestrastate.GuardSpawn wired by core/CLI). A non-nil error blocks the
// spawn; the error text must contain an unblock path.
type SpawnGuard func(subagentType string) error

// ContractRefsGuard is the Contract Epoch gate (spec §5.3, wired to
// orchestrastate.GuardWorkOrderContract): verifies a worker WorkOrder's
// contract_refs against EPOCH.yaml at spawn and again on success.
type ContractRefsGuard func(refs []contract.Ref) error

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
	RouteTaskType          TaskTypeRouter
	GuardSpawn             SpawnGuard
	GuardContractRefs      ContractRefsGuard
	// QuestionAsker enables the runtime Question Barrier (spec §4.3):
	// open_questions[] from task_result are relayed to the user without an
	// orchestrator turn. Nil = barrier off (e.g. core stdio mode).
	QuestionAsker tools.QuestionAsker
	// MaxClarificationRounds caps user round-trips per phase (default 2).
	MaxClarificationRounds int
	// RelayViaLLM disables the runtime barrier (questions stay in the
	// result for the orchestrator to handle — legacy/debug mode).
	RelayViaLLM bool
	// PhaseTimeouts are the resolved orchestra.phase_timeouts values
	// (spec §4.5): stale-phase advisories, Lead brief cap, blocked escalation.
	PhaseTimeouts orchestrastate.PhaseTimeouts
	// MaxWorkerRetries caps validation/final failures for worker children (orchestra).
	MaxWorkerRetries int
	// MaxWorkerVerifyRetries is how many times to re-run the worker after verify failure (default 1).
	MaxWorkerVerifyRetries int
	// WorkerVerifyAffectedTests runs `go test` on packages the worker edited (default true).
	WorkerVerifyAffectedTests *bool
	// WorkerVerifyFrontendTypecheck runs `tsc --noEmit` when frontend files were edited (default true).
	WorkerVerifyFrontendTypecheck *bool
	// WorkerVerifyEnabled disables deterministic post-worker checks when false.
	WorkerVerifyEnabled *bool
	// WorkerLLMVerifyEnabled runs a read-only verifier child after deterministic checks pass (default false).
	WorkerLLMVerifyEnabled *bool
	// TierEscalation re-runs a failing WorkOrder on a senior tier (spec §5.5).
	TierEscalation TierEscalationSettings
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

// Cancellation causes (resilience audit P4): recorded via
// context.WithCancelCause so runChild can report *why* a child was cancelled
// instead of collapsing everything into context.Canceled.
var (
	// ErrCauseStaleContract marks children cancelled by an EPOCH change.
	ErrCauseStaleContract = errors.New("cancelled: contract epoch changed (stale contract_refs)")
	// ErrCauseUserCancel marks children cancelled by an explicit task_cancel.
	ErrCauseUserCancel = errors.New("cancelled: explicit task_cancel")
	// ErrCauseWaitAbandoned marks children cancelled because the parent's
	// task_wait timed out or the parent turn ended.
	ErrCauseWaitAbandoned = errors.New("cancelled: parent stopped waiting (wait timeout or turn end)")
	// ErrCauseShutdown marks children cancelled by TaskRunner.Close.
	ErrCauseShutdown = errors.New("cancelled: task runner shutting down")
)

// childReapTimeout bounds how long Wait/Close block for a cancelled child
// goroutine to actually exit. A tool stuck in a syscall that ignores ctx
// must not freeze the orchestrator turn forever (resilience audit P5).
var childReapTimeout = 30 * time.Second

type taskEntry struct {
	id     string
	cancel context.CancelCauseFunc
	done   chan struct{}
	result *agent.SubtaskResult
	// editPaths is the normalized WorkOrder edit scope for worker tasks;
	// used by the disjoint check (spec §5.6) to serialize conflicting spawns.
	editPaths map[string]struct{}
	// contractRefs pins the running worker to contract artifact versions;
	// used by InvalidateStaleContractTasks on epoch change (spec §5.3).
	contractRefs []contract.Ref
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
	case "verifier":
		return agent.ModeVerifier
	case "product":
		return agent.ModeProduct
	case "documentation":
		return agent.ModeDocs
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

	r.applyTaskTypeRoute(&req)
	subagentType := req.SubagentType
	if subagentType == "" {
		subagentType = "explore"
	}
	if r.child.GuardSpawn != nil {
		if err := r.child.GuardSpawn(subagentType); err != nil {
			return "", err
		}
	}
	var editPaths map[string]struct{}
	var contractRefs []contract.Ref
	if strings.EqualFold(subagentType, "worker") {
		goal := strings.TrimSpace(req.Goal)
		if goal != "" && json.Valid([]byte(goal)) {
			wo, err := ParseWorkOrderJSON(goal)
			if err != nil {
				return "", err
			}
			if r.child.GuardContractRefs != nil {
				if err := r.child.GuardContractRefs(wo.ContractRefs); err != nil {
					return "", err
				}
			}
			// Brief completeness gate (spec §6.2): active only when the
			// dept playbook opted in via brief_required_fields.
			if err := checkBriefCompleteness(r.toolRunner.WorkspaceRoot(), wo); err != nil {
				return "", err
			}
			editPaths = normalizeEditPathSet(EditScopePaths(wo))
			contractRefs = wo.ContractRefs
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
	var cancel context.CancelCauseFunc
	// lead_brief_s (spec §4.5): architecture children without an explicit
	// timeout get the Lead brief wall-clock cap in orchestrated sessions.
	effectiveTimeoutMS := r.leadBriefTimeoutMS(subagentType, req.TimeoutMS)
	if effectiveTimeoutMS > 0 {
		var tcancel context.CancelFunc
		taskCtx, tcancel = context.WithTimeout(parent, time.Duration(effectiveTimeoutMS)*time.Millisecond)
		cancelable, ccancel := context.WithCancelCause(taskCtx)
		taskCtx = cancelable
		cancel = func(cause error) {
			ccancel(cause)
			tcancel()
		}
	} else {
		taskCtx, cancel = context.WithCancelCause(parent)
	}

	entry := &taskEntry{
		id:           taskID,
		cancel:       cancel,
		done:         make(chan struct{}),
		editPaths:    editPaths,
		contractRefs: contractRefs,
	}

	// Disjoint check (spec §5.6): collect running worker tasks whose edit
	// scope intersects ours. Registration and conflict collection happen
	// under one lock, so two overlapping spawns cannot both see a clear
	// field. Each task waits only for tasks registered before it — the
	// wait graph is acyclic by construction.
	r.mu.Lock()
	conflicts := r.conflictingTasksLocked(editPaths)
	r.tasks[taskID] = entry
	r.mu.Unlock()

	go func() {
		defer close(entry.done)
		defer cancel(nil)
		// Resilience audit P1: a panic escaping runChild (agent loop, prompt
		// assembly, verification pipeline) in this goroutine would kill the
		// whole core process — parent orchestrator, sibling workers and the
		// RPC server. Contain it and surface it as a normal task error.
		defer func() {
			if rec := recover(); rec != nil {
				fmt.Fprintf(os.Stderr, "tasks: child %s panicked: %v\n%s\n", taskID, rec, debug.Stack())
				r.mu.Lock()
				if entry.result == nil {
					entry.result = &agent.SubtaskResult{
						TaskID: taskID,
						Status: "error",
						Error:  fmt.Sprintf("child agent panicked: %v", rec),
					}
				}
				r.mu.Unlock()
				if r.child.NotifyAgentEvent != nil {
					r.child.NotifyAgentEvent(map[string]any{
						"type":                "child_done",
						"task_id":             taskID,
						"parent_tool_call_id": req.ParentToolCallID,
						"subagent_type":       subagentType,
						"status":              "error",
						"error":               fmt.Sprintf("child agent panicked: %v", rec),
					})
				}
			}
		}()

		if len(conflicts) > 0 {
			r.notifyQueued(taskID, req.ParentToolCallID, conflicts)
			for _, c := range conflicts {
				select {
				case <-c.done:
				case <-taskCtx.Done():
					r.mu.Lock()
					entry.result = &agent.SubtaskResult{
						TaskID: taskID,
						Status: "timeout",
						Error:  "cancelled while queued behind a conflicting WorkOrder (overlapping target_files)",
					}
					r.mu.Unlock()
					return
				}
			}
		}

		result := r.runChild(taskCtx, taskID, req, subagentType, maxSteps, childTools)

		r.mu.Lock()
		entry.result = result
		r.mu.Unlock()
	}()

	return taskID, nil
}

// conflictingTasksLocked returns unfinished tasks whose edit scope overlaps
// paths. Caller must hold r.mu.
func (r *TaskRunner) conflictingTasksLocked(paths map[string]struct{}) []*taskEntry {
	if len(paths) == 0 {
		return nil
	}
	var out []*taskEntry
	for _, e := range r.tasks {
		if len(e.editPaths) == 0 {
			continue
		}
		select {
		case <-e.done:
			continue
		default:
		}
		for p := range paths {
			if _, hit := e.editPaths[p]; hit {
				out = append(out, e)
				break
			}
		}
	}
	return out
}

func (r *TaskRunner) notifyQueued(taskID, parentToolCallID string, conflicts []*taskEntry) {
	if r.child.NotifyAgentEvent == nil {
		return
	}
	ids := make([]string, 0, len(conflicts))
	for _, c := range conflicts {
		ids = append(ids, c.id)
	}
	r.child.NotifyAgentEvent(map[string]any{
		"type":                "child_queued",
		"task_id":             taskID,
		"parent_tool_call_id": parentToolCallID,
		"waiting_for":         ids,
		"reason":              "overlapping target_files; serialized per spec §5.6",
	})
}

// normalizeEditPathSet builds the comparable path set for the disjoint check.
func normalizeEditPathSet(paths []string) map[string]struct{} {
	if len(paths) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		p = filepath.ToSlash(strings.TrimSpace(p))
		p = strings.TrimPrefix(p, "./")
		if p != "" {
			out[p] = struct{}{}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// applyTaskTypeRoute fills empty spawn fields from the orchestra_routing.yaml
// rule for req.TaskType. Explicit caller values always win; for workers the
// provider/model binding is left to ResolveTier (tier band precedence).
func (r *TaskRunner) applyTaskTypeRoute(req *agent.SubtaskSpawnRequest) {
	if req == nil || strings.TrimSpace(req.TaskType) == "" || r.child.RouteTaskType == nil {
		return
	}
	route, ok := r.child.RouteTaskType(strings.TrimSpace(req.TaskType))
	if !ok {
		return
	}
	if strings.TrimSpace(req.SubagentType) == "" && route.SubagentType != "" {
		req.SubagentType = route.SubagentType
	}
	if strings.TrimSpace(req.Tier) == "" && route.Tier != "" {
		req.Tier = route.Tier
	}
	worker := strings.EqualFold(strings.TrimSpace(req.SubagentType), "worker")
	if !worker && strings.TrimSpace(req.Provider) == "" && strings.TrimSpace(req.Model) == "" {
		req.Provider = route.Provider
		req.Model = route.Model
	}
}

// isLeadGradeSubagent reports whether subagentType is an L4 department lead
// (spec §2.1): resolves the "lead" tier binding instead of worker bands.
func isLeadGradeSubagent(subagentType string) bool {
	switch strings.ToLower(strings.TrimSpace(subagentType)) {
	case "product", "documentation":
		return true
	}
	return false
}

func (r *TaskRunner) resolveChildLLM(req agent.SubtaskSpawnRequest, subagentType string) (llm.Client, string, string) {
	provider := strings.TrimSpace(req.Provider)
	model := strings.TrimSpace(req.Model)
	if provider == "" && model == "" && r.child.ResolveTier != nil {
		if strings.EqualFold(subagentType, "worker") {
			if p, m, ok := r.child.ResolveTier(req.Tier); ok {
				provider, model = p, m
			}
		} else if isLeadGradeSubagent(subagentType) {
			// L4 leads: explicit tier from spawn wins, else the "lead" band.
			// Unbound → fall through to the parent (orchestrator) client.
			tier := strings.TrimSpace(req.Tier)
			if tier == "" {
				tier = "lead"
			}
			if p, m, ok := r.child.ResolveTier(tier); ok {
				provider, model = p, m
			}
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
	var workOrder *WorkOrder
	if strings.EqualFold(subagentType, "worker") {
		goal := strings.TrimSpace(req.Goal)
		if goal != "" && json.Valid([]byte(goal)) {
			wo, err := ParseWorkOrderJSON(goal)
			if err != nil {
				return &agent.SubtaskResult{TaskID: taskID, Status: "error", Error: err.Error()}
			}
			workOrder = wo
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
		eventTier := strings.TrimSpace(req.Tier)
		if eventTier == "" && isLeadGradeSubagent(subagentType) {
			eventTier = "lead" // L4 badge in UI even without explicit spawn tier
		}
		r.child.NotifyAgentEvent(map[string]any{
			"type":                 "child_started",
			"task_id":              taskID,
			"parent_tool_call_id":  req.ParentToolCallID,
			"subagent_type":        subagentType,
			"tier":                 eventTier,
			"model":                modelLabel,
			"content":              req.Goal,
		})
	}
	if mode == agent.ModeWorker && r.child.MaxWorkerRetries > 0 {
		opts.MaxFinalFailures = r.child.MaxWorkerRetries
		opts.MaxInvalidRetries = r.child.MaxWorkerRetries
		opts.MaxToolErrorRepeats = r.child.MaxWorkerRetries
	}
	childGoal := FormatChildGoal(subagentType, req.Tier, req.Goal)
	if conv := loadProjectConventions(r.toolRunner.WorkspaceRoot(), mode); conv != "" {
		childGoal = conv + "\n\n" + childGoal
	}
	if dec := loadDecisionLog(r.toolRunner.WorkspaceRoot(), mode); dec != "" {
		childGoal = dec + "\n\n" + childGoal
	}
	if les := loadDeptLessons(r.toolRunner.WorkspaceRoot(), mode, workOrder); les != "" {
		childGoal = les + "\n\n" + childGoal
	}
	if pb := loadDeptPlaybook(r.toolRunner.WorkspaceRoot(), mode, workOrder); pb != "" {
		childGoal = pb + "\n\n" + childGoal
	}
	if mode == agent.ModeWorker {
		if wo, err := ParseWorkOrderJSON(childGoal); err == nil {
			opts.WorkerEditPaths = EditScopePaths(wo)
			// WorkOrder-driven worker → schema-enforced task_result
			// (spec checklist 31, local L3/L1 drift protection).
			opts.WorkerStrictResult = true
		}
	}
	var hist []llm.Message
	var res *agent.Result
	var runErr error
	if mode == agent.ModeWorker {
		hist, res, runErr = r.runWorkerWithVerification(ctx, client, opts, childGoal)
	} else {
		ag, err := agent.New(client, r.validator, r.toolRunner, opts)
		if err != nil {
			return &agent.SubtaskResult{TaskID: taskID, Status: "error", Error: err.Error()}
		}
		hist, res, runErr = ag.Run(ctx, nil, childGoal)
	}
	status := "done"
	errMsg := ""
	if runErr != nil {
		status, errMsg = classifyChildRunErr(ctx, runErr)
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
		r.recordWorkerToDeptScratchpad(workOrder, "", status, errMsg)
		out := &agent.SubtaskResult{TaskID: taskID, Status: status, Error: errMsg}
		if mode == agent.ModeWorker {
			if hint := recordWorkerLesson(r.toolRunner.WorkspaceRoot(), workOrder, hist, errMsg, status); hint != "" {
				out.Result = annotateLessonPromoteSuggestion(`{"status":"error"}`, hint)
			}
		}
		return out
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

	// Question Barrier (spec §4.3): relay open_questions[] to the user via
	// the runtime, append answers to decisions.md, attach them to the result.
	taskResult = r.relayOpenQuestions(ctx, taskResult)
	taskResult = r.attachPlaybookPromoteHints(taskResult, workOrder)

	// Phase timeouts (spec §4.5): stale-phase advisory + blocked escalation.
	taskResult = r.annotatePhaseTimeout(taskResult)
	taskResult = r.trackBlockedEscalation(ctx, taskResult)

	// Re-check contract_refs on success (spec §5.3): the contract may have
	// changed while the worker ran; a stale result must not reach the Lead
	// as success — staged patches are dropped with the dry-run overlay.
	if mode == agent.ModeWorker && workOrder != nil && r.child.GuardContractRefs != nil {
		if err := r.child.GuardContractRefs(workOrder.ContractRefs); err != nil {
			msg := "stale_contract: contract changed during execution — result discarded, Lead must regenerate the WorkOrder; " + err.Error()
			r.recordWorkerToDeptScratchpad(workOrder, "", "stale_contract", err.Error())
			out := &agent.SubtaskResult{TaskID: taskID, Status: "error", Error: msg}
			if mode == agent.ModeWorker {
				if hint := recordWorkerLesson(r.toolRunner.WorkspaceRoot(), workOrder, hist, msg, "error"); hint != "" {
					out.Result = annotateLessonPromoteSuggestion(`{"status":"error","reason":"stale_contract"}`, hint)
				}
			}
			return out
		}
	}

	// Doc debt (spec §2.3.2): verified worker edits that hit a MANIFEST
	// trigger put the mapped doc into state.md doc_debt for 6b.
	if mode == agent.ModeWorker && workerTaskResultSuccess(taskResult) {
		recordDocDebt(r.toolRunner.WorkspaceRoot(), CollectEditedPaths(hist, ""))
	}

	r.recordWorkerToDeptScratchpad(workOrder, taskResult, "done", "")
	if mode == agent.ModeWorker {
		if hint := recordWorkerLesson(r.toolRunner.WorkspaceRoot(), workOrder, hist, taskResult, "done"); hint != "" {
			taskResult = annotateLessonPromoteSuggestion(taskResult, hint)
		}
	}
	return &agent.SubtaskResult{TaskID: taskID, Status: "done", Result: taskResult}
}

// classifyChildRunErr maps a child run error to a SubtaskResult status,
// using context.Cause to distinguish deliberate cancellations (task_cancel,
// epoch invalidation, shutdown) from timeouts and real failures.
func classifyChildRunErr(ctx context.Context, runErr error) (status, errMsg string) {
	errMsg = runErr.Error()
	if !errors.Is(runErr, context.DeadlineExceeded) && !errors.Is(runErr, context.Canceled) {
		return "error", errMsg
	}
	cause := context.Cause(ctx)
	switch {
	case errors.Is(cause, ErrCauseStaleContract),
		errors.Is(cause, ErrCauseUserCancel),
		errors.Is(cause, ErrCauseWaitAbandoned),
		errors.Is(cause, ErrCauseShutdown):
		return "cancelled", cause.Error()
	default:
		// Plain deadline (task timeout / lead brief cap) or parent turn end.
		return "timeout", errMsg
	}
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
		entry.cancel(ErrCauseWaitAbandoned)
		// Wait for the child goroutine to exit before returning so callers
		// (and t.TempDir cleanup on Windows) do not race with late writes
		// under .orchestra/. Bounded (resilience audit P5): a tool stuck in
		// a syscall that ignores ctx must not freeze the parent turn forever.
		reap := time.NewTimer(childReapTimeout)
		defer reap.Stop()
		select {
		case <-entry.done:
		case <-reap.C:
			// Leave the entry registered so Close() can still observe it;
			// report a zombie instead of blocking the orchestrator.
			fmt.Fprintf(os.Stderr, "tasks: child %s did not exit %s after cancel — reporting zombie\n", taskID, childReapTimeout)
			return &agent.SubtaskResult{
				TaskID: taskID,
				Status: "error",
				Error:  fmt.Sprintf("child did not exit %s after cancellation (stuck tool call?); it was left to terminate in the background", childReapTimeout),
			}, nil
		}
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
// InvalidateStaleContractTasks cancels running worker tasks whose
// contract_refs no longer match EPOCH.yaml — the spec §5.3 "смена epoch →
// task_cancel + drop staged patches" rule. Staged patches live in the child's
// dry-run overlay, so cancellation discards them without touching disk.
// Returns the cancelled task IDs (sorted, for deterministic logs).
func (r *TaskRunner) InvalidateStaleContractTasks(_ context.Context) []string {
	if r == nil || r.child.GuardContractRefs == nil {
		return nil
	}
	type candidate struct {
		id     string
		refs   []contract.Ref
		cancel context.CancelCauseFunc
	}
	r.mu.Lock()
	var cands []candidate
	for id, e := range r.tasks {
		if len(e.contractRefs) == 0 {
			continue
		}
		select {
		case <-e.done:
			continue
		default:
		}
		cands = append(cands, candidate{id: id, refs: e.contractRefs, cancel: e.cancel})
	}
	r.mu.Unlock()

	var cancelled []string
	for _, c := range cands {
		if err := r.child.GuardContractRefs(c.refs); err != nil {
			c.cancel(ErrCauseStaleContract)
			cancelled = append(cancelled, c.id)
		}
	}
	sort.Strings(cancelled)
	return cancelled
}

func (r *TaskRunner) Cancel(_ context.Context, taskID string) error {
	r.mu.Lock()
	entry, ok := r.tasks[taskID]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("task %q not found", taskID)
	}
	entry.cancel(ErrCauseUserCancel)
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
		e.cancel(ErrCauseShutdown)
	}
	// Bounded reap (resilience audit P5): a zombie child (tool stuck in a
	// syscall that ignores ctx) must not hang core shutdown forever.
	deadline := time.NewTimer(childReapTimeout)
	defer deadline.Stop()
	for _, e := range entries {
		select {
		case <-e.done:
		case <-deadline.C:
			fmt.Fprintf(os.Stderr, "tasks: Close: child %s did not exit %s after cancel — abandoning\n", e.id, childReapTimeout)
			return
		}
	}
}
