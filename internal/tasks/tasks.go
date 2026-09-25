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
	"github.com/orchestra/orchestra/internal/app"
	"github.com/orchestra/orchestra/internal/contract"
	"github.com/orchestra/orchestra/internal/orchestrastate"
	"github.com/orchestra/orchestra/internal/roles"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/internal/tools/fs"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
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
// spawn; the error text must contain an unblock path. ctx is the spawner's:
// the guard reads the project as the spawner sees it (tools.Runner.View).
type SpawnGuard func(ctx context.Context, subagentType string) error

// ContractRefsGuard is the Contract Epoch gate (spec §5.3, wired to
// orchestrastate.GuardWorkOrderContract): verifies a worker WorkOrder's
// contract_refs at spawn, against the spawner's view, and again on success
// and on every contract change, against the worker's own.
type ContractRefsGuard func(ctx context.Context, refs []contract.Ref) error

// ChildAgentConfig holds history/memory settings propagated to child agents.
type ChildAgentConfig struct {
	// Settings are what a child takes from .orchestra.yml: the loop's
	// breakers, BytesPerContextToken and the permission rules. The fields
	// below override the budgets for children. Nil leaves the agent's
	// defaults, as in tests that build a runner by hand.
	Settings *app.Settings

	MaxPromptBytes         int
	CompactThresholdPct    int
	ModelContextTokens     int
	CompletionMaxTokens    int
	ToolDigestBytes        int
	HistoryPruneKeepRecent int
	UsageTracker           agent.UsageRecorder
	ProviderLabel          string
	ModelLabel             string
	// CompactionClient is the cheap model used for a child's own history
	// compaction (llm.router.fast_provider / providers.fast). Nil = the child
	// compacts with whatever model it is running on, which on a long worker
	// run means paying the main model to summarise its own transcript.
	CompactionClient        llm.Client
	CompactionContextTokens int
	Caps                    tools.Capabilities
	ResolveClient           ChildClientResolver
	ResolveTier             TierResolver
	RouteTaskType           TaskTypeRouter
	GuardSpawn              SpawnGuard
	GuardContractRefs       ContractRefsGuard
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
	// Budget caps the turn's tree of tasks (agent.turn_budget).
	Budget TurnBudget
	// RunID is the turn the tasks belong to (its turn_id). With each task's
	// identity it attributes the children's llm_log lines; when empty the
	// run id is taken from the spawner's ctx.
	RunID string
	// AgentLogger writes the children's tool_call / tool_result events to
	// llm_log.jsonl. Without it a worker's writes were invisible: the log
	// showed the child's LLM requests and nothing it did with the answers.
	AgentLogger *llm.Logger
	// OnChildEvent, when set, receives streaming events from child agents (E2E metrics).
	OnChildEvent func(agent.AgentEvent)
	// ChildEventSink builds a per-task OnEvent handler with child scope metadata.
	// Preferred over OnChildEvent in production core wiring.
	ChildEventSink func(taskID, parentToolCallID, subagentType string) func(agent.AgentEvent)
	// NotifyAgentEvent emits arbitrary agent/event payloads (child lifecycle).
	NotifyAgentEvent func(params map[string]any)
	// Agency is the resolved agency: section (AgencyFromConfig): who may
	// delegate to or message whom, nesting depth, concurrency, budgets. The
	// zero value keeps the one-level runner (no nested spawn, no messages)
	// with no concurrency cap.
	Agency AgencySettings
	// Agents are the custom agents (agents: in .orchestra.yml) a parent may
	// start by name as subagent_type.
	Agents []AgentProfile
}

// TaskRunner implements agent.SubtaskRunner using real child agents.
//
// Without the agency (ChildAgentConfig.Agency.Enabled=false) children cannot
// spawn further subtasks. With it, a child delegates and talks along the
// agency flows down to Agency.MaxDepth — the Orchestrator → Dept Lead →
// Worker shape of the spec — through a scopedRunner that carries its place
// in the tree. Every task of the turn, at any depth, is registered here: one
// board, one disjoint-scope check, one set of concurrency slots.
type TaskRunner struct {
	llmClient  llm.Client
	validator  *schema.Validator
	toolRunner *tools.Runner
	child      ChildAgentConfig

	mu    sync.Mutex
	tasks map[string]*taskEntry
	seq   int

	// all keeps every task of the turn, collected ones included, for
	// task_board and for depends_on on a task that was already waited for.
	all []*taskEntry
	// byKey names tasks for depends_on, per spawner (keyOf): two Leads that
	// both call their first WorkOrder wo-1 each depend on their own.
	byKey map[string]*taskEntry
	// slots are the per-depth concurrency semaphores (Agency.MaxParallel).
	slots map[int]chan struct{}
	// messages counts send_message + agent_post against Agency.MaxMessages.
	messages int
	// rootInbox holds notes for the top-level agent, drained on its next step.
	rootInbox []agent.InboxMessage
	// barrierMu puts one round of questions to the user at a time, and
	// answered keeps this turn's answers by question, so a question two
	// children both return is asked once (question_barrier.go).
	barrierMu sync.Mutex
	answered  map[string]string
	// closed is set by Close. A spawn after it is refused: a relay or a
	// task_spawn racing the end of the turn used to register into the fresh
	// map Close left behind, was never cancelled, and edited the workspace
	// during the next turn.
	closed bool
	// firstSpawn starts the tree's wall clock (TurnBudget.MaxWall); wallTimer
	// cancels what is still running when it runs out.
	firstSpawn time.Time
	wallTimer  *time.Timer
	// storeMu serialises the inbox and thread files under .orchestra/agency.
	storeMu sync.Mutex
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
	// ErrRunnerClosed refuses a spawn after TaskRunner.Close: the turn it
	// belonged to is over.
	ErrRunnerClosed = errors.New("the turn has ended; no new subagent can start")
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
	// layer is where the task writes (tools.Runner.ForkLayer): nil outside a
	// dry run. Its view is what the task's contract is checked against.
	layer *fs.Overlay

	// Agency bookkeeping, guarded by TaskRunner.mu.
	key          string       // name for depends_on (WorkOrder task_id)
	address      string       // who the child is: department, custom agent or role
	role         string       // built-in role it runs as
	parent       string       // address of the spawner
	parentTaskID string       // task of the spawner ("" = the root)
	spawner      string       // task that spawned it ("" = the root); a relayed worker's is its Lead, its parent the Lead's owner
	depth        int          // root children = 1
	deps         []*taskEntry // must succeed before this one starts
	goal         string       // first line of the goal, for the board
	status       string       // queued|waiting_deps|running|done|error|timeout|cancelled
	started      time.Time
	finished     time.Time
	worker       bool
	fingerprint  string               // role + goal, to spot a task started twice (budget.go)
	edited       []string             // files a successful worker changed
	inbox        []agent.InboxMessage // live notes for the running child
}

// New creates a new TaskRunner.
func New(llmClient llm.Client, validator *schema.Validator, toolRunner *tools.Runner, child ChildAgentConfig) *TaskRunner {
	return &TaskRunner{
		llmClient:  llmClient,
		validator:  validator,
		toolRunner: toolRunner,
		child:      child,
		tasks:      make(map[string]*taskEntry),
		byKey:      make(map[string]*taskEntry),
		slots:      make(map[int]chan struct{}),
	}
}

func childToolsForSubagent(subagentType string, caps tools.Capabilities) []llm.ToolDef {
	defs := tools.ListToolsForMode(string(modeForSubagent(subagentType)), caps, false, false)
	// debug and general reuse the top-level mode lists, which include the
	// repo-mutating git tools. A child must not have them: it shares the
	// parent's working tree.
	return ensureTaskResult(tools.StripRepoMutatingTools(defs))
}

func ensureTaskResult(defs []llm.ToolDef) []llm.ToolDef {
	for _, d := range defs {
		if d.Function.Name == "task_result" {
			return defs
		}
	}
	return append(defs, tools.ToolTaskResult())
}

// modeForSubagent is the mode a child of this type runs in: a built-in role
// by its registry name (explore when none is given), anything else — a
// custom agent — under its own name.
func modeForSubagent(subagentType string) agent.Mode {
	t := strings.ToLower(strings.TrimSpace(subagentType))
	if t == "" {
		return agent.ModeExplore
	}
	if _, ok := roles.Lookup(t); ok {
		return agent.Mode(t)
	}
	return agent.Mode(subagentType)
}

// childTier is the model band a child of this type runs on (roles.Spec.Tier).
func childTier(subagentType string) roles.Tier {
	spec, ok := roles.Lookup(strings.ToLower(strings.TrimSpace(subagentType)))
	if !ok {
		return roles.TierParent
	}
	return spec.Tier
}

// DefaultChildMaxSteps is the hard cap on child agent loop iterations when
// ChildAgentConfig.MaxStepsCap is unset.
const DefaultChildMaxSteps = 12

// DefaultTaskTimeoutMS is used for sync `task` and for `task_spawn` when the
// model omits timeout_ms (avoids orphan background children). The agent
// applies it (agent.Options.ChildTimeoutMS, from agent.child_timeout_s).
const DefaultTaskTimeoutMS = agent.DefaultChildTimeoutMS

// Spawn creates a new child agent task and starts it in a goroutine. Called
// on the TaskRunner itself it spawns as the root agent (the hub); children
// spawn through their scopedRunner.
func (r *TaskRunner) Spawn(ctx context.Context, req agent.SubtaskSpawnRequest) (string, error) {
	return r.spawnFrom(ctx, rootScope(), req, spawnExtra{})
}

// spawnExtra carries what only the runtime sets on a spawn.
type spawnExtra struct {
	// history is the recipient's earlier conversation with the sender
	// (send_message threads); nil for a fresh child.
	history []llm.Message
	// verb names the act in refusals ("delegate to", "message").
	verb string
	// owner, when set, is the agent the task belongs to — who may wait for
	// and cancel it — in place of the spawner. The batch relay spawns as the
	// Lead (its flows, its department) on behalf of the Lead's parent.
	owner *agentScope
}

func (r *TaskRunner) spawnFrom(ctx context.Context, from agentScope, req agent.SubtaskSpawnRequest, extra spawnExtra) (string, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return "", ErrRunnerClosed
	}
	r.seq++
	taskID := fmt.Sprintf("task_%d_%d", r.seq, time.Now().UnixNano()%100000)
	r.mu.Unlock()

	r.applyTaskTypeRoute(&req)
	dept := strings.TrimSpace(req.Dept)
	target, err := r.resolveTarget(req.SubagentType, dept)
	if err != nil {
		return "", err
	}
	// A Lead's workers work for the Lead's department unless told otherwise:
	// its scratchpad, playbook and lessons are theirs too. Only workers — a
	// scout the Lead sends out is not the department, and must not answer to
	// its address or read its inbox.
	if dept == "" && from.dept != "" && strings.EqualFold(target.role, "worker") {
		dept = from.dept
		target.address = dept
	}
	verb := extra.verb
	if verb == "" {
		verb = "delegate to"
	}
	if err := r.checkReach(from, target, verb); err != nil {
		return "", err
	}
	if req.ReadOnlyChildren && target.changesFiles() {
		return "", fmt.Errorf("%s: %s can change files, and this turn only reads (plan, architecture and ask modes); delegate to explore, scout, ask or verifier, and describe the change in your answer",
			from.address, target.address)
	}
	if p := target.profile; p != nil {
		if strings.TrimSpace(req.Provider) == "" && strings.TrimSpace(req.Model) == "" && strings.TrimSpace(req.Tier) == "" {
			req.Provider, req.Model, req.Tier = p.Provider, p.Model, p.Tier
		}
		if req.MaxSteps <= 0 && p.MaxSteps > 0 {
			req.MaxSteps = p.MaxSteps
		}
	}

	capSteps := r.child.MaxStepsCap
	if capSteps <= 0 {
		capSteps = DefaultChildMaxSteps
	}
	maxSteps := req.MaxSteps
	if maxSteps <= 0 || maxSteps > capSteps {
		maxSteps = capSteps
	}

	if r.child.GuardSpawn != nil {
		// The phase guard judges by role. A custom agent on a read-only base
		// that was given write tools is a writer, whatever its base says.
		guardRole := target.role
		if agent.ReadOnlyRole(guardRole) && target.changesFiles() {
			guardRole = "general"
		}
		if err := r.child.GuardSpawn(ctx, guardRole); err != nil {
			return "", err
		}
	}
	isWorker := strings.EqualFold(target.role, "worker")
	key := strings.TrimSpace(req.Key)
	dependsOn := append([]string(nil), req.DependsOn...)
	var editPaths map[string]struct{}
	var contractRefs []contract.Ref
	if isWorker {
		goal := strings.TrimSpace(req.Goal)
		if goal != "" && json.Valid([]byte(goal)) {
			goal = withDefaultScratchpad(goal, dept)
			req.Goal = goal
			wo, err := ParseWorkOrderJSON(goal)
			if err != nil {
				return "", err
			}
			if r.child.GuardContractRefs != nil {
				if err := r.child.GuardContractRefs(ctx, wo.ContractRefs); err != nil {
					return "", err
				}
			}
			// Brief completeness gate (spec §6.2): active only when the
			// dept playbook opted in via brief_required_fields.
			if err := checkBriefCompleteness(r.toolRunner.WorkspaceRoot(), r.toolRunner.View(ctx), wo); err != nil {
				return "", err
			}
			editPaths = normalizeEditPathSet(EditScopePaths(wo))
			contractRefs = wo.ContractRefs
			if key == "" {
				key = strings.TrimSpace(wo.TaskID)
			}
			dependsOn = append(dependsOn, wo.DependsOn...)
		}
	}

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
	effectiveTimeoutMS := r.leadBriefTimeoutMS(target.role, req.TimeoutMS)
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
		key:          key,
		address:      target.address,
		role:         target.role,
		parent:       from.address,
		parentTaskID: from.taskID,
		spawner:      from.taskID,
		depth:        from.depth + 1,
		goal:         firstLine(req.Goal, 120),
		status:       "queued",
		started:      time.Now(),
		worker:       isWorker,
		fingerprint:  taskFingerprint(target.address, req.Goal),
	}
	if extra.owner != nil {
		entry.parent = extra.owner.address
		entry.parentTaskID = extra.owner.taskID
	}
	// The child's model calls and tool calls are logged under its own
	// identity (llm.Trace), not its parent's: llm_log.jsonl is shared by the
	// whole tree, and this is what tells its lines apart.
	runID := r.child.RunID
	if runID == "" {
		runID = llm.TraceFrom(parent).RunID
	}
	taskCtx = llm.WithTrace(taskCtx, llm.Trace{
		RunID:        runID,
		TaskID:       taskID,
		ParentTaskID: entry.parentTaskID,
		Depth:        entry.depth,
	})
	// The task writes into its own layer over its spawner's view (ORC-1): the
	// edits reach the spawner only when the task succeeds (commitLayer), and
	// go with it otherwise. Reads see the spawner's view live, so a task
	// waiting on dependencies sees what they committed.
	layer := r.toolRunner.ForkLayer(parent)
	taskCtx = tools.WithLayer(taskCtx, layer)
	entry.layer = layer

	// Disjoint check (spec §5.6): collect running worker tasks whose edit
	// scope intersects ours. Registration and conflict collection happen
	// under one lock, so two overlapping spawns cannot both see a clear
	// field. Each task waits only for tasks registered before it — the
	// wait graph is acyclic by construction, and so is the depends_on
	// graph: a dependency must already be registered.
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		cancel(nil)
		return "", ErrRunnerClosed
	}
	if err := r.admitLocked(entry.fingerprint); err != nil {
		r.mu.Unlock()
		cancel(nil)
		return "", err
	}
	deps, depErr := r.resolveDepsLocked(from.taskID, dependsOn)
	if depErr != nil {
		r.mu.Unlock()
		cancel(nil)
		return "", depErr
	}
	entry.deps = deps
	r.startWallClockLocked()
	conflicts := r.conflictingTasksLocked(editPaths)
	r.tasks[taskID] = entry
	r.all = append(r.all, entry)
	if key != "" {
		// A re-spawned WorkOrder takes its key over: later dependents mean
		// the attempt that is still to come, not the one that failed.
		r.byKey[keyOf(from.taskID, key)] = entry
	}
	r.mu.Unlock()

	childScope := from.child(target.address, target.role, target.name, taskID)
	childScope.dept = dept

	go func() {
		defer close(entry.done)
		// Before done closes: whoever waits for the task sees its edits
		// committed, or gone. A task that committed has nothing left to drop.
		defer r.toolRunner.DropLayer(tools.LayerContext(parent), layer)
		defer cancel(nil)
		defer r.markFinished(entry)
		// Every task closes with exactly one child_done, however it ended:
		// ran to a result, failed a dependency, was cancelled while queued
		// behind a conflict or waiting for a slot, or panicked. A task that
		// never announced its end stayed "running" in the UI forever and left
		// a hole in the tree the log is read back into.
		defer func() {
			r.mu.Lock()
			res := entry.result
			r.mu.Unlock()
			r.notifyChildDone(entry, req.ParentToolCallID, target.name, res)
		}()
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
			}
		}()

		upstream := ""
		if len(deps) > 0 {
			r.setStatus(entry, "waiting_deps")
			var res *agent.SubtaskResult
			upstream, res = r.awaitDeps(taskCtx, entry)
			if res != nil {
				r.mu.Lock()
				entry.result = res
				r.mu.Unlock()
				return
			}
		}

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

		release, err := r.acquireSlot(taskCtx, entry, req.ParentToolCallID)
		if err != nil {
			r.mu.Lock()
			entry.result = &agent.SubtaskResult{
				TaskID: taskID,
				Status: "timeout",
				Error:  "cancelled while waiting for a free slot (agency.max_parallel)",
			}
			r.mu.Unlock()
			return
		}
		defer release()
		r.setStatus(entry, "running")

		result := r.runChild(taskCtx, taskID, req, target, childScope, maxSteps, extra.history, upstream)

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
	if childTier(req.SubagentType) != roles.TierWorker && strings.TrimSpace(req.Provider) == "" && strings.TrimSpace(req.Model) == "" {
		req.Provider = route.Provider
		req.Model = route.Model
	}
}

func (r *TaskRunner) resolveChildLLM(req agent.SubtaskSpawnRequest, subagentType string) (llm.Client, string, string) {
	provider := strings.TrimSpace(req.Provider)
	model := strings.TrimSpace(req.Model)
	if provider == "" && model == "" && r.child.ResolveTier != nil {
		switch childTier(subagentType) {
		case roles.TierWorker:
			if p, m, ok := r.child.ResolveTier(req.Tier); ok {
				provider, model = p, m
			}
		case roles.TierLead:
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

func (r *TaskRunner) runChild(ctx context.Context, taskID string, req agent.SubtaskSpawnRequest, target spawnTarget, scope agentScope, maxSteps int, history []llm.Message, upstream string) *agent.SubtaskResult {
	subagentType := target.role
	childTools := r.childToolsForTarget(target, scope)
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
	opts := app.ChildOptions(r.child.Settings, func(o *agent.Options) {
		o.MaxSteps = maxSteps
		o.MaxPromptBytes = maxPrompt
		o.CompactThresholdPct = r.child.CompactThresholdPct
		o.ModelContextTokens = r.child.ModelContextTokens
		o.CompletionMaxTokens = r.child.CompletionMaxTokens
		o.ToolDigestBytes = r.child.ToolDigestBytes
		o.HistoryPruneKeepRecent = r.child.HistoryPruneKeepRecent
		o.LLMStepTimeout = r.child.LLMStepTimeout
		o.AgentLogger = r.child.AgentLogger
		o.CompactionClient = r.child.CompactionClient
		o.CompactionContextTokens = r.child.CompactionContextTokens
		o.CustomTools = childTools
		o.Mode = mode
		o.Dept = scope.dept
		o.UsageTracker = r.child.UsageTracker
		// Children run on their own tier model, which may be a different
		// family from the parent's; ChildOptions takes the prompt family from
		// ModelLabel.
		o.ProviderLabel = providerLabel
		o.ModelLabel = modelLabel
		// Workers: no parent dialog, no project memory inject, no session notes.
		o.AutoSessionMemory = false
		o.SkipMemoryInject = mode == agent.ModeWorker
		o.AllowExec = r.child.Caps.Exec
		o.AllowWeb = r.child.Caps.Web
		o.AllowBrowser = r.child.Caps.Browser
		if mode == agent.ModeWorker {
			wsOff := false
			o.WorkingState = &wsOff
			o.TurnDigestKeep = 0
			o.AssistantPrefill = "{"
		}
	})
	if r.child.OnChildEvent != nil {
		opts.OnEvent = r.child.OnChildEvent
	}
	if r.child.ChildEventSink != nil {
		opts.OnEvent = r.child.ChildEventSink(taskID, req.ParentToolCallID, subagentType)
	}
	if r.child.Agency.Enabled {
		opts.SubtaskRunner = &scopedRunner{r: r, s: scope}
	}
	if r.child.NotifyAgentEvent != nil {
		eventTier := strings.TrimSpace(req.Tier)
		if eventTier == "" && childTier(subagentType) == roles.TierLead {
			eventTier = "lead" // L4 badge in UI even without explicit spawn tier
		}
		ev := map[string]any{
			"type":                "child_started",
			"task_id":             taskID,
			"parent_tool_call_id": req.ParentToolCallID,
			"subagent_type":       target.name,
			"tier":                eventTier,
			"model":               modelLabel,
			"content":             req.Goal,
			"agent":               scope.address,
			"depth":               scope.depth,
		}
		if len(scope.chain) > 1 {
			ev["parent_agent"] = scope.chain[len(scope.chain)-2]
		}
		if scope.parentTaskID != "" {
			ev["parent_task_id"] = scope.parentTaskID
		}
		r.child.NotifyAgentEvent(ev)
	}
	if mode == agent.ModeWorker && r.child.MaxWorkerRetries > 0 {
		opts.MaxFinalFailures = r.child.MaxWorkerRetries
		opts.MaxInvalidRetries = r.child.MaxWorkerRetries
		opts.MaxToolErrorRepeats = r.child.MaxWorkerRetries
	}
	childGoal := FormatChildGoal(subagentType, req.Tier, req.Goal)
	if mode == agent.ModeWorker {
		childGoal = exploreFirstWorkerPolicy(workOrder) + "\n\n" + childGoal
	}
	if conv := loadProjectConventions(r.toolRunner.View(ctx), mode); conv != "" {
		childGoal = conv + "\n\n" + childGoal
	}
	if dec := loadDecisionLog(r.toolRunner.WorkspaceRoot(), mode); dec != "" {
		childGoal = dec + "\n\n" + childGoal
	}
	if les := loadDeptLessons(r.toolRunner.WorkspaceRoot(), mode, workOrder); les != "" {
		childGoal = les + "\n\n" + childGoal
	}
	if pb := loadDeptPlaybook(r.toolRunner.View(ctx), mode, workOrder); pb != "" {
		childGoal = pb + "\n\n" + childGoal
	}
	// Agency context, outermost first in reading order: who the child is,
	// what its department already knows, what it was told while idle, and
	// what the tasks it waited for produced.
	if upstream != "" {
		childGoal = upstream + "\n\n" + childGoal
	}
	// A department's inbox and scratchpad are its Lead's: workers share the
	// address to hear each other live, but must not consume the notes left
	// for the Lead.
	if mode != agent.ModeWorker {
		if notes := r.takeInbox(scope.address); len(notes) > 0 {
			text, rest := agent.FitAgentMessages(notes, agencyInboxInjectMaxBytes)
			childGoal = text + "\n\n" + childGoal
			// What did not fit goes to the child's live inbox: it reads
			// them on its first steps instead of never.
			if len(rest) > 0 {
				r.mu.Lock()
				if e := r.findEntryLocked(taskID); e != nil {
					e.inbox = append(append([]agent.InboxMessage(nil), rest...), e.inbox...)
				}
				r.mu.Unlock()
			}
		}
		if sp := loadDeptScratchpadForLead(r.toolRunner.WorkspaceRoot(), scope.dept); sp != "" {
			childGoal = sp + "\n\n" + childGoal
		}
	}
	if target.profile != nil && target.profile.SystemPrompt != "" {
		childGoal = formatAgentRole(target.profile) + "\n\n" + childGoal
	}
	if mode == agent.ModeWorker && workOrder != nil {
		opts.WorkerEditPaths = EditScopePaths(workOrder)
		// WorkOrder-driven worker → schema-enforced task_result
		// (spec checklist 31, local L3/L1 drift protection).
		opts.WorkerStrictResult = true
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
		hist, res, runErr = ag.Run(ctx, append([]llm.Message(nil), history...), childGoal)
	}
	// Notes that arrived after the child's last step would be lost with it;
	// they go to its inbox for the next agent at this address. A worker's
	// leftovers are sibling chatter about a batch that is over — dropped
	// rather than handed to the department's next Lead.
	if mode != agent.ModeWorker {
		r.flushLiveInbox(taskID, scope.address)
	} else {
		r.drainTaskInbox(taskID)
	}
	status := "done"
	errMsg := ""
	if runErr != nil {
		status, errMsg = classifyChildRunErr(ctx, runErr)
	}
	if runErr != nil {
		r.recordWorkerToDeptScratchpad(workOrder, "", status, errMsg)
		// Attach what the child did manage to do. Without it the parent sees
		// only an error string and redoes the whole task from nothing —
		// including the reads the child already paid for.
		if progress := agent.FormatSubagentProgress(subagentType, req.Goal, hist, r.child.ToolDigestBytes); progress != "" {
			errMsg = errMsg + "\n\n" + progress
		}
		out := &agent.SubtaskResult{TaskID: taskID, Status: status, Error: errMsg}
		if mode == agent.ModeWorker {
			if hint := recordWorkerLesson(r.toolRunner.WorkspaceRoot(), workOrder, hist, errMsg, status); hint != "" {
				out.Result = annotateLessonPromoteSuggestion(`{"status":"error"}`, hint)
			}
		}
		return out
	}

	// A task that is not a worker has succeeded once its run did: its edits go
	// to its owner now, before anything it relays (a Lead's WorkOrders) starts
	// on top of them.
	if mode != agent.ModeWorker {
		if err := r.commitLayer(ctx); err != nil {
			return &agent.SubtaskResult{TaskID: taskID, Status: "error", Error: err.Error()}
		}
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
	// It runs before the relay below: workers used to start on the Lead's
	// WorkOrders while its blocking questions were still open (ORC-9).
	taskResult, held := r.relayOpenQuestions(ctx, taskResult)

	// A Dept Lead that returns batch_workorders[] hands them to the runtime
	// (spec §3.7, §5.6): the workers are spawned, run and verified here, and
	// the Lead's result carries their outcome up. A batch written before its
	// blocking questions were answered waits for the Lead to revise it.
	if held {
		taskResult = holdBatchWorkOrders(taskResult)
	} else {
		taskResult = r.relayBatchWorkOrders(ctx, scope, target, taskResult)
	}
	taskResult = r.attachPlaybookPromoteHints(taskResult, workOrder)

	// Phase timeouts (spec §4.5): stale-phase advisory + blocked escalation.
	taskResult = r.annotatePhaseTimeout(taskResult)
	taskResult = r.trackBlockedEscalation(ctx, taskResult)

	// Re-check contract_refs on success (spec §5.3): the contract may have
	// changed while the worker ran; a stale result must not reach the Lead
	// as success, and its edits are dropped with its layer.
	if mode == agent.ModeWorker && workOrder != nil && r.child.GuardContractRefs != nil {
		if err := r.child.GuardContractRefs(ctx, workOrder.ContractRefs); err != nil {
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

	// A worker's edits reach its owner only when it verified: a failed,
	// blocked or unverified worker's layer is dropped (ORC-1).
	if mode == agent.ModeWorker && workerOutcomeSucceeded(taskResult) {
		if err := r.commitLayer(ctx); err != nil {
			msg := err.Error() + " — the worker's edits were discarded; re-plan the WorkOrder against the current files"
			r.recordWorkerToDeptScratchpad(workOrder, "", "error", msg)
			return &agent.SubtaskResult{TaskID: taskID, Status: "error", Error: msg}
		}
	}

	// Doc debt (spec §2.3.2): verified worker edits that hit a MANIFEST
	// trigger put the mapped doc into state.md doc_debt for 6b.
	if mode == agent.ModeWorker && workerOutcomeSucceeded(taskResult) {
		edited := CollectEditedPaths(hist, "")
		recordDocDebt(r.toolRunner.WorkspaceRoot(), edited)
		r.recordEdited(taskID, edited)
	}

	r.recordWorkerToDeptScratchpad(workOrder, taskResult, "done", "")
	if mode == agent.ModeWorker {
		if hint := recordWorkerLesson(r.toolRunner.WorkspaceRoot(), workOrder, hist, taskResult, "done"); hint != "" {
			taskResult = annotateLessonPromoteSuggestion(taskResult, hint)
		}
	}
	return &agent.SubtaskResult{TaskID: taskID, Status: "done", Result: taskResult}
}

// commitLayer merges the layer of the task behind ctx into its owner's. A
// conflict — a file another task committed a change to since this one first
// wrote it — is the task's error, and its layer is dropped. So is a task
// that was cancelled on the way: its contract went stale, or its spawner
// ended, and what it did goes with it.
func (r *TaskRunner) commitLayer(ctx context.Context) error {
	layer := fs.OverlayFrom(ctx)
	if layer == nil {
		return nil
	}
	if cause := context.Cause(ctx); cause != nil {
		return fmt.Errorf("not committed: %w", cause)
	}
	paths, err := layer.Commit()
	if err != nil {
		var conflict *fs.MergeConflict
		if errors.As(err, &conflict) {
			return fmt.Errorf("merge_conflict: %w", err)
		}
		return err
	}
	r.afterContractCommit(ctx, layer, paths)
	return nil
}

// afterContractCommit is the epoch hook for a task's commit (spec §5.3). An
// owner's change to a contract artifact is written in its task's layer, and
// becomes the contract when that layer commits into the turn: the runtime
// records it in EPOCH.yaml then. Every commit of an artifact, into the turn
// or into a Lead's layer, cancels the running workers that now see a version
// their WorkOrder was not written against — their layers go with them.
func (r *TaskRunner) afterContractCommit(ctx context.Context, layer *fs.Overlay, paths []string) {
	touched := false
	for _, p := range paths {
		if _, ok := contract.ArtifactFileName(p); ok {
			touched = true
			break
		}
	}
	if !touched {
		return
	}
	if !layer.Owner().IsLayer() {
		root := r.toolRunner.WorkspaceRoot()
		if _, err := contract.Refresh(root, r.toolRunner.View(ctx), paths); err != nil {
			fmt.Fprintf(os.Stderr, "tasks: contract epoch update after commit: %v\n", err)
		}
	}
	r.InvalidateStaleContractTasks(ctx)
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
		errors.Is(cause, ErrCauseShutdown),
		errors.Is(cause, ErrCauseTurnBudget):
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

// Wait blocks until the task completes. When its timeout or ctx runs out
// first, it gives up on the task and cancels it — the synchronous task tool,
// whose wait is the child's lifetime.
func (r *TaskRunner) Wait(ctx context.Context, taskID string, timeoutMS int) (*agent.SubtaskResult, error) {
	return r.wait(ctx, taskID, timeoutMS, true)
}

// Poll is task_wait: it waits up to timeoutMS for the task and, when the
// task is still running then, says so (status still_running) and leaves it
// running. The model can wait again, do other work, or task_cancel it. Before
// this, a task_wait timeout cancelled the child, and a model that polled a
// long worker with a short timeout killed it without knowing (audit ORC-11).
// A cancelled ctx — the turn ending — still cancels the task.
func (r *TaskRunner) Poll(ctx context.Context, taskID string, timeoutMS int) (*agent.SubtaskResult, error) {
	return r.wait(ctx, taskID, timeoutMS, false)
}

// stillRunning is Poll's answer for a task that did not finish in time.
func stillRunning(taskID string, waited time.Duration) *agent.SubtaskResult {
	return &agent.SubtaskResult{
		TaskID: taskID,
		Status: "still_running",
		Error:  fmt.Sprintf("still running after %s; it keeps running — task_wait again later, or task_cancel it if you no longer need it", waited.Round(time.Millisecond)),
	}
}

func (r *TaskRunner) wait(ctx context.Context, taskID string, timeoutMS int, giveUp bool) (*agent.SubtaskResult, error) {
	r.mu.Lock()
	entry, ok := r.tasks[taskID]
	if !ok {
		// Collected by an earlier wait: task_board still lists it, so
		// answering "not found" contradicted the board. Its result stands.
		if e := r.findEntryLocked(taskID); e != nil && !e.finished.IsZero() {
			res := e.result
			r.mu.Unlock()
			if res == nil {
				res = &agent.SubtaskResult{TaskID: taskID, Status: "error", Error: "task produced no result"}
			}
			return res, nil
		}
		r.mu.Unlock()
		return nil, fmt.Errorf("task %q not found", taskID)
	}
	r.mu.Unlock()

	var timeout <-chan time.Time
	if timeoutMS > 0 {
		t := time.NewTimer(time.Duration(timeoutMS) * time.Millisecond)
		defer t.Stop()
		timeout = t.C
	}

	select {
	case <-entry.done:
		return r.collect(entry), nil
	case <-timeout:
		if !giveUp {
			return stillRunning(taskID, time.Duration(timeoutMS)*time.Millisecond), nil
		}
		return r.abandon(entry, context.DeadlineExceeded), nil
	case <-ctx.Done():
		return r.abandon(entry, ctx.Err()), nil
	}
}

// collect returns a finished task's result and unregisters it.
func (r *TaskRunner) collect(entry *taskEntry) *agent.SubtaskResult {
	r.mu.Lock()
	result := entry.result
	r.mu.Unlock()
	r.removeTask(entry.id)
	if result == nil {
		return &agent.SubtaskResult{TaskID: entry.id, Status: "error", Error: "task produced no result"}
	}
	return result
}

// abandon cancels a task its waiter gave up on and returns what it ended with.
func (r *TaskRunner) abandon(entry *taskEntry, why error) *agent.SubtaskResult {
	taskID := entry.id
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
		}
	}
	r.mu.Lock()
	result := entry.result
	r.mu.Unlock()
	r.removeTask(taskID)
	if result != nil {
		return result
	}
	if errors.Is(why, context.DeadlineExceeded) {
		return &agent.SubtaskResult{TaskID: taskID, Status: "timeout", Error: "wait timeout"}
	}
	return &agent.SubtaskResult{TaskID: taskID, Status: "cancelled", Error: why.Error()}
}

// InvalidateStaleContractTasks cancels running worker tasks whose
// contract_refs no longer match the contract they see — the spec §5.3 "смена
// epoch → task_cancel + drop staged patches" rule. Each worker is checked
// against its own view, so a change still in its owner's layer cancels only
// the workers under that owner. A cancelled worker does not commit: its
// layer, and every edit in it, is dropped when it ends.
// Returns the cancelled task IDs (sorted, for deterministic logs).
func (r *TaskRunner) InvalidateStaleContractTasks(_ context.Context) []string {
	if r == nil || r.child.GuardContractRefs == nil {
		return nil
	}
	type candidate struct {
		id     string
		refs   []contract.Ref
		layer  *fs.Overlay
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
		cands = append(cands, candidate{id: id, refs: e.contractRefs, layer: e.layer, cancel: e.cancel})
	}
	r.mu.Unlock()

	var cancelled []string
	for _, c := range cands {
		if err := r.child.GuardContractRefs(tools.WithLayer(context.Background(), c.layer), c.refs); err != nil {
			c.cancel(ErrCauseStaleContract)
			cancelled = append(cancelled, c.id)
		}
	}
	sort.Strings(cancelled)
	return cancelled
}

// Cancel aborts a running task.
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
	r.closed = true
	if r.wallTimer != nil {
		r.wallTimer.Stop()
	}
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
