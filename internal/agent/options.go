package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/orchestra/orchestra/internal/agent/working"
	configpkg "github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/patch/ops"
	"github.com/orchestra/orchestra/patch/patches"
	"github.com/orchestra/orchestra/protocol/schema"

	"github.com/orchestra/orchestra/internal/hooks"
	"github.com/orchestra/orchestra/internal/memory"
	"github.com/orchestra/orchestra/internal/permission"
	"github.com/orchestra/orchestra/llm"
)

// PermissionRequester / PermissionRequest / PermissionResponse are
// re-exported aliases of internal/permission so existing callers
// (agent.Options.PermissionRequester, every comparison against
// PermissionResponse{Approved:false}) compile unchanged. H6 in
// architecture audit moved the canonical declarations into
// internal/permission to remove a copy in core that needed an adapter.
type (
	PermissionRequester = permission.Requester
	PermissionRequest   = permission.Request
	PermissionResponse  = permission.Response
)

// HooksRunner executes pre/post tool call hooks.
//
// RunPreTool answers with a decision rather than an error because a hook has
// more than one thing to say: it can deny with a reason the model can act on,
// or hand back a corrected input instead of only refusing.
type HooksRunner interface {
	RunPreTool(ctx context.Context, toolName string, input json.RawMessage) HookDecision
	RunPostTool(ctx context.Context, toolName string, output json.RawMessage)
	// RunLifecycle reports an event that is not about a tool call. The agent
	// raises pre_compact; the rest are the caller's, because only it knows
	// where a session or a turn begins.
	RunLifecycle(ctx context.Context, event string, payload json.RawMessage) HookDecision
}

// HookDecision is hooks.Decision, aliased the way PermissionRequester is, so
// implementations and the tests that fake them do not each need the import.
type HookDecision = hooks.Decision

// SubtaskRunner manages child agent tasks spawned via task.spawn.
type SubtaskRunner interface {
	Spawn(ctx context.Context, req SubtaskSpawnRequest) (string, error)
	Wait(ctx context.Context, taskID string, timeoutMS int) (*SubtaskResult, error)
	Cancel(ctx context.Context, taskID string) error
}

// SubtaskSpawnRequest is the request for spawning a child agent task.
type SubtaskSpawnRequest struct {
	Goal         string
	SubagentType string // explore|ask|debug|architecture|verifier|general|worker (or ListToolsForMode key)
	MaxSteps     int
	TimeoutMS    int
	// Provider / Model optionally override the child LLM (named providers: map entry).
	Provider string
	Model    string
	// Tier selects orchestra.tiers[] when SubagentType is "worker" (e.g. complex|focused|micro).
	Tier string
	// TaskType is the Orchestra routing key (orchestra_routing.yaml). When set,
	// empty SubagentType/Tier/Provider/Model default from the routing rule.
	TaskType string
	// ParentToolCallID is the parent agent's tool_call_id for task/task_spawn.
	ParentToolCallID string
}

// SkillSpec is a thin summary of a discovered skill, used for system-prompt
// advertisement and validation of skill_invoke calls. The full skill body
// is not embedded here — SkillRunner.InvokeSkill resolves it by name.
type SkillSpec struct {
	Name        string
	Description string
}

// SkillRunner runs a named skill synchronously as a child agent and
// returns its result text. Returns an error if the skill is unknown or
// the child agent fails.
type SkillRunner interface {
	InvokeSkill(ctx context.Context, name, task string) (string, error)
}

// SubtaskResult is the result returned by a completed child agent task.
type SubtaskResult struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"` // "done" | "cancelled" | "error"
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Mode is the agent's execution-policy selector. M6 in architecture
// audit elevated this from raw strings to a typed alias so the compiler
// flags accidental mode comparisons against arbitrary strings; the
// underlying type is still string so JSON / YAML / config flow is
// unaffected.
//
// Unknown values are accepted by Options.Mode (no panic / no error) and
// fall through to the build-mode default inside the tools registry —
// this matches the pre-existing behaviour and keeps the CLI's silent-
// fallback contract intact for ad-hoc / custom-agent modes that may
// arrive later. Callers that need strict validation should use
// IsKnownMode.
type Mode string

// Agent mode constants.
const (
	ModeBuild        Mode = "build"         // default: full tool access
	ModePlan         Mode = "plan"          // read-only + plan tools
	ModeExplore      Mode = "explore"       // grep/glob/read only (subagent)
	ModeAsk          Mode = "ask"           // Q&A read-only
	ModeDebug        Mode = "debug"         // root-cause + targeted fix
	ModeArchitecture Mode = "architecture"  // design / plan md only
	ModeGeneral      Mode = "general"       // multi-step execution subagent: full read+write tools, returns via task_result.
	ModeAgent        Mode = "agent"         // auto-route to build|plan|explore before Run
	ModeOrchestra    Mode = "orchestra"     // Lead planner; delegates to worker tiers
	ModeWorker       Mode = "worker"        // atomic WorkOrder executor (child only)
	ModeVerifier     Mode = "verifier"      // goal-backward read-only verification (child only)
	ModeProduct      Mode = "product"       // Product Lead: PRD/user stories in .orchestra/product/ only (child only)
	ModeDocs         Mode = "documentation" // Docs Lead: L1 conventions.md, MANIFEST, docs/ scaffold+content (child only)
	ModeCompaction   Mode = "compaction"    // internal: compresses history into a summary.
	ModeTitle        Mode = "title"         // internal: generates a short task title from the user query.
	ModeSummary      Mode = "summary"       // internal: produces a brief summary of completed work.
)

// IsKnownMode reports whether m is one of the built-in modes.
//
// The registry lives in internal/config (config.BuiltInModeKind) because the
// tools and CLI layers need it too and cannot import this package. Keeping a
// second list here is what let `product` and `documentation` exist as real
// modes while being absent from the reserved-name check, so a custom agent or
// skill could take their names.
//
// A false result does NOT mean "invalid": a custom agent from .orchestra.yml
// is passed through Options.Mode under its own name and falls through to
// build-mode tools by design.
func IsKnownMode(m Mode) bool {
	return configpkg.IsBuiltInMode(string(m))
}

// IsChildOnlyMode reports whether m is a subagent role with its own input
// contract (WorkOrder, task_result) rather than a mode a user starts.
func IsChildOnlyMode(m Mode) bool {
	kind, ok := configpkg.BuiltInModeKind(string(m))
	return ok && kind == configpkg.ModeKindChildOnly
}

type Options struct {
	MaxSteps int
	// MaxInvalidRetries is the number of extra attempts after an invalid JSON/schema output.
	MaxInvalidRetries int
	// MaxPromptBytes limits the total prompt size passed to the LLM.
	MaxPromptBytes int

	// Apply controls whether to write changes to disk.
	// If false, the agent will run fs.apply_ops in dry-run mode and return diffs.
	Apply  bool
	Backup bool

	// AllowExec bypasses all exec consent checks вЂ” equivalent to --allow-exec (debug/override).
	AllowExec bool
	// ExecAllow is a per-command allowlist (basename, e.g. "go", "npm").
	// If non-empty, exec.run is shown to the model and only listed commands are allowed.
	ExecAllow []string
	// ExecDeny is a per-command denylist (takes precedence over ExecAllow).
	ExecDeny []string

	// AllowWeb enables the webfetch tool вЂ” equivalent to --allow-web.
	AllowWeb bool

	// AllowBrowser enables browser.* tools вЂ” equivalent to --allow-browser.
	AllowBrowser bool

	// PermissionRules is the ordered per-tool permission ruleset from config.permissions.
	// Evaluated before the AllowExec / AllowWeb gates; first matching rule wins.
	// allow в†’ permit even without --allow-exec/--allow-web.
	// deny  в†’ always block with TOOL_DENIED.
	// No match в†’ fall through to existing gates.
	PermissionRules []configpkg.PermissionRule

	// InitialTodos is the model's task checklist loaded from the session at turn start.
	InitialTodos []tools.TodoItem

	// MaxDeniedToolRepeats is a hard stop to prevent infinite TOOL_DENIED loops
	// (e.g. model repeatedly calling exec.run when it's not allowed).
	MaxDeniedToolRepeats int
	// MaxToolErrorRepeats is a hard stop for repeated TOOL_ERR loops.
	MaxToolErrorRepeats int
	// MaxFinalFailures is a hard stop for repeated resolve/apply failures after "final".
	MaxFinalFailures int
	// LLMStepTimeout bounds time spent waiting for the model per attempt.
	LLMStepTimeout time.Duration

	// AssistantPrefill appends a partial assistant message before each LLM step
	// (OpenAI prefilling). The prefix is merged into text responses when the
	// model omits it. Used for Worker JSON determinism (default "{" in worker mode).
	AssistantPrefill string

	// ResponseFormat, if non-nil, is sent to the LLM on every call (grammar-constrained sampling).
	ResponseFormat *llm.ResponseFormat
	// PromptFamily selects model-family-specific system prompt. Auto-detected if empty.
	PromptFamily string
	// SystemPromptOverride, if non-empty, replaces the built-in mode system prompt.
	// .orchestra/system.txt still takes precedence when present.
	SystemPromptOverride string

	// Mode selects the agent role. See the Mode constants
	// (ModeBuild / ModePlan / ModeExplore / вЂ¦) for the registered set.
	// Empty string behaves identically to ModeBuild for backward
	// compatibility; an unknown non-empty value also falls through to
	// build (see IsKnownMode if you want strict rejection).
	Mode Mode

	// QuestionAsker, if non-nil, enables the question tool (interactive user Q&A).
	// Use StdinQuestionAsker for direct CLI mode. Must be nil for orchestra core (stdio conflict).
	QuestionAsker tools.QuestionAsker

	// HumanGates marks human gates (spec §4.4) as required: keys
	// config.GateGitCommit (G2) / config.GateGitPush (G3). A required gate
	// asks the user via QuestionAsker before the tool runs; without an
	// asker the call is denied (fail-closed). Empty map = no gates.
	HumanGates map[string]bool

	// WorkerStrictResult enforces the JSON task_result schema for
	// WorkOrder-driven workers (spec checklist 31): local L3/L1 models must
	// return {"status": ..., ...}, not free text.
	WorkerStrictResult bool

	// StateMaxBytes is the .orchestra/state.md size budget: when an
	// update_working_state write pushes the file above it, the runtime
	// archives the older head to .orchestra/archive/ (spec §6.4).
	// 0 = orchestrastate.DefaultStateMaxBytes.
	StateMaxBytes int

	// JustSwitchedFromPlan, when true, injects a one-shot build-switch reminder on the first step.
	// Set by the caller when restarting an agent in build mode after plan approval.
	JustSwitchedFromPlan bool

	// PlanPath is the session plan markdown file (relative to project root).
	// Used in plan mode write guards and {{PLAN_PATH}} substitution in prompts/tools.
	PlanPath string

	// OnEvent, if non-nil, is called synchronously for each streaming event during a step.
	// Nil disables streaming (agent falls back to the blocking Complete path).
	//
	// Contract: the callback (a) must not block, and (b) MUST be goroutine-safe.
	// runParallelToolBatch fires events from up to parallelBatchWorkerLimit
	// worker goroutines concurrently, so a sink that mutates shared state
	// (UI buffer, log file, channel) needs its own mutex or buffered channel.
	// A panicking callback is recovered (see safeRun) so a buggy sink can't
	// take down the agent loop, but the event is then dropped silently.
	OnEvent func(AgentEvent)

	// OnStepHistory, when set, receives a snapshot of the LLM history after
	// each completed tool step so the caller can persist mid-turn progress
	// (crash recovery, resilience audit P2). Called synchronously from the
	// agent loop — implementations must be fast and/or throttled. The slice
	// is a fresh copy; the callback may retain it.
	OnStepHistory func(step int, history []llm.Message)

	// AgentLogger, if non-nil, writes tool_call / tool_result events to llm_log.jsonl.
	AgentLogger *llm.Logger

	// CustomTools, if non-empty, overrides tools.ListTools() for this agent.
	// Used to give child agents a restricted tool set.
	CustomTools []llm.ToolDef

	// ExtraTools are appended to the standard tool list (e.g. MCP server tools).
	// Ignored when CustomTools is set.
	ExtraTools []llm.ToolDef

	// SubtaskRunner, if non-nil, enables task.spawn/task.wait/task.cancel tools.
	SubtaskRunner SubtaskRunner

	// Skills are the file-based skills discovered for this run. When non-empty
	// AND SkillRunner is non-nil, the agent exposes the skill_invoke tool and
	// lists available skills in the system prompt.
	Skills []SkillSpec

	// SkillRunner, if non-nil, is invoked synchronously by the skill_invoke tool
	// to run a child agent with the named skill's prompt/tools/model/provider.
	SkillRunner SkillRunner

	// IsChild signals that this agent was spawned from another agent via
	// task.spawn or skill_invoke and is expected to finish by emitting a
	// `task_result` tool call. Main agents (set by CLI apply, JSON-RPC
	// agent.run) leave this false вЂ” a `task_result` emitted by a main
	// agent is treated as an invalid call (with a hint to the model) rather
	// than terminating the run. H11 in audit ledger.
	IsChild bool

	// UserImages are PartImage entries attached to the *initial* user message.
	// When non-empty the user message switches from string Content to
	// multimodal Parts (the rendered userPrompt becomes a PartText, followed
	// by each image). Requires the configured LLM to be multimodal.
	UserImages []llm.ContentPart

	// MultimodalLLM is set true when the configured LLM accepts image
	// content. Gates browser.screenshot piping (image bytes returned by
	// the tool are converted to PartImage and injected into history).
	MultimodalLLM bool

	// HooksRunner, if non-nil, runs pre/post tool call hooks.
	HooksRunner HooksRunner

	// Memory configures layered project/session memory injection.
	Memory memory.Config
	// SessionID binds session-scoped memory (.orchestra/memory/sessions/<id>.md).
	SessionID string

	// ToolDigestBytes caps tool results in history; larger outputs become digests (0 = disable).
	ToolDigestBytes int

	// HistoryPruneKeepRecent is how many recent tool-bearing history atoms stay
	// full during retroactive prune (default 2). 0 = prune all eligible.
	HistoryPruneKeepRecent int

	// AutoSessionMemory appends explore/grep notes to session memory automatically.
	AutoSessionMemory bool

	// SkipMemoryInject disables ORCHESTRA.md / agent.md / global memory in the
	// system prompt. Used for workers that should see only the WorkOrder.
	SkipMemoryInject bool

	// PermissionRequester, if non-nil, is consulted before bash/exec.run runs
	// instead of (or before) the static AllowExec gate.
	// Nil в†’ fall through to existing AllowExec / ExecAllow gates (CLI mode).
	PermissionRequester PermissionRequester

	// CompactionClient, if non-nil, is used for ModeCompaction LLM calls
	// (cheap/fast provider). Nil → use the main agent LLM client.
	CompactionClient llm.Client

	// CompactionContextTokens is the context window (num_ctx) of the model
	// CompactionClient actually talks to. When 0 and CompactionClient is set,
	// compactionCorpusBudget falls back to ModelContextTokens (the MAIN
	// model's window) which is wrong whenever the compaction provider has a
	// smaller window — the summarizer request itself can then overflow.
	// Always set this alongside CompactionClient (see Core.compactionClient).
	CompactionContextTokens int

	// CompactThresholdPct, if > 0, triggers history compaction when total history size (in bytes)
	// exceeds this percentage of MaxPromptBytes. 0 = disabled. Recommended: 60.
	// Compaction failure is non-fatal: logs a warning and continues without compacting.
	CompactThresholdPct int

	// ForceCompactOnce forces one compaction at the start of Run regardless of threshold.
	ForceCompactOnce bool

	// ModelContextTokens is the model context window (num_ctx / max_model_len).
	// When > 0, compaction uses PromptBudgetTokens(ctx, CompletionMaxTokens)
	// so history shrinks before vLLM would 400 on prompt+max_tokens.
	ModelContextTokens int

	// CompletionMaxTokens is the configured llm.max_tokens (want). Reserved
	// inside the context window when deciding whether to compact.
	CompletionMaxTokens int

	// BytesPerContextToken calibrates estimatePromptTokens (default 4).
	BytesPerContextToken int

	// WorkingState enables <working_state> inject (default true when unset via ApplyHistoryConfig).
	WorkingState *bool

	// TurnDigestKeep is how many recent turn digests to inject (default 3; 0 = off).
	TurnDigestKeep int

	// TurnDigestEveryN writes a mid-run micro-digest every N agent steps (0 = end-of-run only).
	// Does not compact or rewrite LLM history.
	TurnDigestEveryN int

	// WorkerEditPaths limits edit/write to WorkOrder target_file(s) when ModeWorker.
	// Empty = no path restriction (legacy / free-form goals).
	WorkerEditPaths []string

	Debug  bool
	Logger *log.Logger

	// UsageTracker, if non-nil, receives token-usage records after every LLM
	// completion (both Complete and CompleteStream paths). Wired by the CLI
	// `apply` entry-point and shared across pipeline stages / subagents so
	// the final usage.jsonl record covers the whole run.
	UsageTracker UsageRecorder

	// ProviderLabel is the human label for the active provider/model recorded
	// alongside each usage tick (e.g. "openai" / "fast" / "anthropic"). When
	// empty, "openai" is used.
	ProviderLabel string
	// ModelLabel is the active model id reported to the tracker. When empty,
	// "unknown" is used. Custom agents / pipeline stages can override this.
	ModelLabel string

	// Profile is an adaptive execution preset ("fast" / "precision").
	// Empty means no preset. See ApplyProfile.
	Profile string
}

// UsageRecorder is the agent's view of the usage tracker. Mirrors
// *usage.Tracker.Record to keep the agent package free of a hard import on
// internal/usage.
//
// M5 in audit ledger: Record may be invoked concurrently from the main
// agent loop AND from in-process child agents (skill_invoke, task_spawn)
// that share the same tracker вЂ” implementations MUST be goroutine-safe.
// usage.Tracker satisfies this via an internal mutex; custom recorders
// (test fakes, alternative metrics sinks) need to as well.
type UsageRecorder interface {
	Record(provider, model string, prompt, completion int)
	// RecordCost is Record plus the provider-reported cost in USD (0 when the
	// provider does not report cost; implementations then fall back to their
	// own pricing, if any).
	RecordCost(provider, model string, prompt, completion int, providerCostUSD float64)
}

// PromptCacheRecorder is the optional extension of UsageRecorder for the
// prompt-cache split (cached reads / cache writes) that Anthropic and
// OpenAI-compatible gateways report. It is a separate interface so the
// recorders that predate it keep compiling; the agent forwards the counters
// only when the recorder can take them. usage.Tracker implements it.
type PromptCacheRecorder interface {
	RecordPromptCache(provider, model string, cachedPrompt, cacheWrite int)
}

// AgentEvent wraps a streaming event with agent-level context.
type AgentEvent struct {
	Step   int
	Stream llm.StreamEvent
}

type Result struct {
	Steps int

	Patches []patches.Patch
	Ops     []ops.AnyOp

	Applied bool
	// ApplyResponse is returned from fs.apply_ops (dry-run or write).
	ApplyResponse *tools.FSApplyOpsResponse

	// Todos is the model's updated task checklist after this run.
	Todos []tools.TodoItem

	// SubtaskResult is set when a child agent completed via task.result tool call.
	SubtaskResult string

	// SwitchToBuild is set when plan_exit was approved by the user.
	// The caller should restart the agent in Mode "build" with JustSwitchedFromPlan=true.
	SwitchToBuild bool

	// MaxStepsExceeded is true when the run stopped at the step limit but
	// staged/partial results were flushed (dry-run overlay or apply path).
	MaxStepsExceeded bool

	// StopReason is a stable machine token for the TUI:
	//   "completed" | "partial" | "max_steps"
	StopReason string

	// RuleSuggestion is set when this turn's anti-pattern repeated on the
	// same file often enough (lessons.RuleSuggestThreshold) to offer the
	// human a rule for ORCHESTRA.md, instead of the count being silently
	// discarded. nil on every other turn.
	RuleSuggestion *RuleSuggestion
}

// RuleSuggestion is a human-facing offer to turn a repeated anti-pattern
// into a project instruction. Unlike lesson_promote/playbook_promote (an
// LLM tool call gated by a free-text Question Barrier answer, targeting a
// dept playbook), this is a direct suggestion surfaced to the person at the
// keyboard, targeting ORCHESTRA.md.
type RuleSuggestion struct {
	Dept   string
	File   string
	Count  int
	Verify string
	// RuleLine is the exact line to append to the project's instructions
	// file if the human accepts.
	RuleLine string
	// Text is the human-readable prompt shown in chat.
	Text string
}

// ActiveProviderReporter is implemented by clients that can move between
// providers mid-run (llm.FallbackClient). Usage records follow the report so
// the ledger names the provider that actually answered.
type ActiveProviderReporter interface {
	ActiveProvider() string
}

type Agent struct {
	llm llm.Client
	// activeProvider is set when llm can move between providers; nil otherwise.
	activeProvider ActiveProviderReporter
	validator      *schema.Validator
	tools          *tools.Runner
	opts           Options
	todos      []tools.TodoItem // current turn's working todo list
	ckgContext string           // pre-fetched CKG nodes block, empty if unavailable

	// justSwitchedFromPlan is true for the first nextStep call after planв†’build switch.
	// Cleared after the reminder is injected so it fires at most once.
	justSwitchedFromPlan bool

	// toolDefsCache memoises buildToolDefs result. Inputs (opts.Mode,
	// AllowExec/Web/Browser, SubtaskRunner, QuestionAsker, Skills,
	// ExtraTools, CustomTools) are fixed at agent construction time, so
	// the slice is identical across every nextStep call. P1 in audit
	// ledger (Sprint 6): previously rebuilt every step вЂ” ~50 tool defs
	// Г— N steps re-serialised on each LLM request.
	toolDefsOnce  sync.Once
	toolDefsCache []llm.ToolDef

	// ctxBreakdownOnce memoises the fixed part of the context breakdown
	// (system prompt / rules / tool defs / skills sizes) — same inputs as
	// toolDefsCache, stable for the agent's lifetime.
	ctxBreakdownOnce  sync.Once
	ctxBreakdownFixed []ctxCategory

	// diags tracks LSP diagnostic fingerprints per file across write/
	// edit attempts so a model that re-writes the same file with the
	// same compile errors is detected even when its tool arguments
	// differ. H7 in architecture audit: replaces the Sprint 6 prompt-
	// only signal with a structural check.
	diags *DiagTracker

	// turnMutatingTools counts write/edit calls in the current Run so
	// premature-final rejection does not block legitimate finals after a
	// denied or failed mutating attempt.
	turnMutatingTools int

	// exploreFirstSatisfied is set after read/grep/explore (explore-first gate).
	exploreFirstSatisfied bool

	// lastPromptTokens is the most recent real Usage.PromptTokens (0 if unknown).
	lastPromptTokens int

	// lastPromptBytes is the serialized size of the last request sent to the
	// LLM. Paired with lastPromptTokens it calibrates bytes-per-token.
	lastPromptBytes int

	// calibratedBytesPerToken is learned from real usage (0 = not yet known).
	calibratedBytesPerToken int

	// detectedBytesPerToken is the pre-calibration guess derived from the
	// script of the text actually being sent (0 = not yet measured). It only
	// covers step 1; from step 2 on, calibratedBytesPerToken supersedes it.
	detectedBytesPerToken int

	// overflowRecoveries counts compact→retry cycles triggered by provider
	// context-window rejections during this Run.
	overflowRecoveries int

	// llmInfraErr is set when an LLM call fails at connect (unreachable).
	// Compaction must not issue another LLM request after this.
	llmInfraErr error

	// contextPressureWarned is set after emitting a soft approaching-threshold
	// notice once this Run (avoid per-step spam).
	contextPressureWarned bool

	// compactMetrics accumulates compaction stats for this agent instance.
	compactMetrics CompactMetrics

	// working is the rule-based ledger for the current Run (token economy).
	working *working.State
}

func New(llmClient llm.Client, v *schema.Validator, toolRunner *tools.Runner, opts Options) (*Agent, error) {
	if llmClient == nil {
		return nil, fmt.Errorf("llm client is nil")
	}
	if v == nil {
		return nil, fmt.Errorf("validator is nil")
	}
	if toolRunner == nil {
		return nil, fmt.Errorf("tools runner is nil")
	}
	ApplyDefaults(&opts)
	ap, _ := llmClient.(ActiveProviderReporter)
	return &Agent{
		llm:                  llmClient,
		activeProvider:       ap,
		validator:            v,
		tools:                toolRunner,
		opts:                 opts,
		justSwitchedFromPlan: opts.JustSwitchedFromPlan,
		diags:                newDiagTracker(),
	}, nil
}
