package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	configpkg "github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/patches"
	"github.com/orchestra/orchestra/internal/ops"
	"github.com/orchestra/orchestra/internal/plan"
	promptpkg "github.com/orchestra/orchestra/internal/prompt"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/resolver"
	"github.com/orchestra/orchestra/internal/schema"
	"github.com/orchestra/orchestra/internal/tools"

	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/memory"
	"github.com/orchestra/orchestra/internal/permission"
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
type HooksRunner interface {
	RunPreTool(ctx context.Context, toolName string, input json.RawMessage) error
	RunPostTool(ctx context.Context, toolName string, output json.RawMessage)
}

// SubtaskRunner manages child agent tasks spawned via task.spawn.
type SubtaskRunner interface {
	Spawn(ctx context.Context, req SubtaskSpawnRequest) (string, error)
	Wait(ctx context.Context, taskID string, timeoutMS int) (*SubtaskResult, error)
	Cancel(ctx context.Context, taskID string) error
}

// SubtaskSpawnRequest is the request for spawning a child agent task.
type SubtaskSpawnRequest struct {
	Goal         string
	SubagentType string // "explore" (default), "general", or another ListToolsForMode key
	MaxSteps     int
	TimeoutMS    int
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
	ModeBuild      Mode = "build"      // default: full tool access
	ModePlan       Mode = "plan"       // read-only + plan tools
	ModeExplore    Mode = "explore"    // grep/glob/read only (subagent)
	ModeGeneral    Mode = "general"    // multi-step execution subagent: full read+write tools, returns via task_result.
	ModeCompaction Mode = "compaction" // internal: compresses history into a summary.
	ModeTitle      Mode = "title"      // internal: generates a short task title from the user query.
	ModeSummary    Mode = "summary"    // internal: produces a brief summary of completed work.
)

// knownModes is the closed set of modes registered in this package.
// Used by IsKnownMode for callers that want a hard fail on a typo
// instead of the registry's silent fall-through to build behaviour.
var knownModes = map[Mode]bool{
	ModeBuild: true, ModePlan: true, ModeExplore: true,
	ModeGeneral: true, ModeCompaction: true, ModeTitle: true, ModeSummary: true,
}

// IsKnownMode reports whether m is a mode this package recognises.
// Callers reading Options.Mode from user-provided config can use this
// to surface a typo immediately rather than wait for the silent build
// fallback to misclassify the run.
func IsKnownMode(m Mode) bool { return knownModes[m] }

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

	// AllowExec bypasses all exec consent checks — equivalent to --allow-exec (debug/override).
	AllowExec bool
	// ExecAllow is a per-command allowlist (basename, e.g. "go", "npm").
	// If non-empty, exec.run is shown to the model and only listed commands are allowed.
	ExecAllow []string
	// ExecDeny is a per-command denylist (takes precedence over ExecAllow).
	ExecDeny []string

	// AllowWeb enables the webfetch tool — equivalent to --allow-web.
	AllowWeb bool

	// AllowBrowser enables browser.* tools — equivalent to --allow-browser.
	AllowBrowser bool

	// PermissionRules is the ordered per-tool permission ruleset from config.permissions.
	// Evaluated before the AllowExec / AllowWeb gates; first matching rule wins.
	// allow → permit even without --allow-exec/--allow-web.
	// deny  → always block with TOOL_DENIED.
	// No match → fall through to existing gates.
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

	// ResponseFormat, if non-nil, is sent to the LLM on every call (grammar-constrained sampling).
	ResponseFormat *llm.ResponseFormat
	// PromptFamily selects model-family-specific system prompt. Auto-detected if empty.
	PromptFamily string
	// SystemPromptOverride, if non-empty, replaces the built-in mode system prompt.
	// .orchestra/system.txt still takes precedence when present.
	SystemPromptOverride string

	// Mode selects the agent role. See the Mode constants
	// (ModeBuild / ModePlan / ModeExplore / …) for the registered set.
	// Empty string behaves identically to ModeBuild for backward
	// compatibility; an unknown non-empty value also falls through to
	// build (see IsKnownMode if you want strict rejection).
	Mode Mode

	// QuestionAsker, if non-nil, enables the question tool (interactive user Q&A).
	// Use StdinQuestionAsker for direct CLI mode. Must be nil for orchestra core (stdio conflict).
	QuestionAsker tools.QuestionAsker

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
	// agent.run) leave this false — a `task_result` emitted by a main
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

	// PermissionRequester, if non-nil, is consulted before bash/exec.run runs
	// instead of (or before) the static AllowExec gate.
	// Nil → fall through to existing AllowExec / ExecAllow gates (CLI mode).
	PermissionRequester PermissionRequester

	// CompactThresholdPct, if > 0, triggers history compaction when total history size (in bytes)
	// exceeds this percentage of MaxPromptBytes. 0 = disabled. Recommended: 70.
	// Compaction failure is non-fatal: logs a warning and continues without compacting.
	CompactThresholdPct int

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
// that share the same tracker — implementations MUST be goroutine-safe.
// usage.Tracker satisfies this via an internal mutex; custom recorders
// (test fakes, alternative metrics sinks) need to as well.
type UsageRecorder interface {
	Record(provider, model string, prompt, completion int)
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
}

type Agent struct {
	llm        llm.Client
	validator  *schema.Validator
	tools      *tools.Runner
	opts       Options
	todos      []tools.TodoItem // current turn's working todo list
	ckgContext string           // pre-fetched CKG nodes block, empty if unavailable

	// justSwitchedFromPlan is true for the first nextStep call after plan→build switch.
	// Cleared after the reminder is injected so it fires at most once.
	justSwitchedFromPlan bool

	// toolDefsCache memoises buildToolDefs result. Inputs (opts.Mode,
	// AllowExec/Web/Browser, SubtaskRunner, QuestionAsker, Skills,
	// ExtraTools, CustomTools) are fixed at agent construction time, so
	// the slice is identical across every nextStep call. P1 in audit
	// ledger (Sprint 6): previously rebuilt every step — ~50 tool defs
	// × N steps re-serialised on each LLM request.
	toolDefsOnce  sync.Once
	toolDefsCache []llm.ToolDef

	// diags tracks LSP diagnostic fingerprints per file across write/
	// edit attempts so a model that re-writes the same file with the
	// same compile errors is detected even when its tool arguments
	// differ. H7 in architecture audit: replaces the Sprint 6 prompt-
	// only signal with a structural check.
	diags *diagTracker
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
	if opts.MaxSteps <= 0 {
		opts.MaxSteps = 24
	}
	if opts.MaxInvalidRetries <= 0 {
		opts.MaxInvalidRetries = 3
	}
	if opts.MaxPromptBytes <= 0 {
		opts.MaxPromptBytes = 64 * 1024
	}
	if opts.MaxDeniedToolRepeats <= 0 {
		opts.MaxDeniedToolRepeats = 2
	}
	if opts.MaxToolErrorRepeats <= 0 {
		opts.MaxToolErrorRepeats = 6
	}
	if opts.MaxFinalFailures <= 0 {
		opts.MaxFinalFailures = 6
	}
	if opts.LLMStepTimeout <= 0 {
		opts.LLMStepTimeout = 25 * time.Second
	}
	return &Agent{
		llm:                  llmClient,
		validator:            v,
		tools:                toolRunner,
		opts:                 opts,
		justSwitchedFromPlan: opts.JustSwitchedFromPlan,
		diags:                newDiagTracker(),
	}, nil
}

// Run executes the agent loop for userQuery and returns the updated history, result, and error.
// Pass a nil history for a fresh (one-shot) run; pass an existing history to continue a session.
func (a *Agent) Run(ctx context.Context, history []llm.Message, userQuery string) ([]llm.Message, *Result, error) {
	userQuery = strings.TrimSpace(userQuery)
	if userQuery == "" {
		return nil, nil, fmt.Errorf("user query is empty")
	}
	// Initialize todos from session state (empty for one-shot runs).
	a.todos = append([]tools.TodoItem(nil), a.opts.InitialTodos...)
	// Pre-fetch relevant CKG nodes once per Run (injected only on step 1).
	a.ckgContext = a.tools.FetchCKGContext(ctx, userQuery)

	if history == nil {
		history = make([]llm.Message, 0, 32)
	}
	steps := 0
	maxStepsReminderSent := false
	cb := NewCircuitBreaker(a.opts.MaxDeniedToolRepeats, a.opts.MaxToolErrorRepeats, a.opts.MaxFinalFailures, a.opts.MaxInvalidRetries)

	emitStepDone := func(reason string) {
		if a.opts.OnEvent != nil && reason != "" {
			a.opts.OnEvent(AgentEvent{Step: steps, Stream: llm.StreamEvent{
				Kind:    llm.StreamEventStepDone,
				Content: reason,
			}})
		}
	}

	for steps < a.opts.MaxSteps {
		steps++

		// M3 in audit ledger: honour parent ctx cancellation at the top of
		// every loop iteration so $/cancelRequest unwinds the run even
		// before the next LLM call sees it. Without this, the loop kept
		// running compaction + nextStep before noticing the cancel.
		if err := ctx.Err(); err != nil {
			return history, nil, err
		}

		// Retroactive prune: shrink older tool outputs already in history.
		if a.opts.ToolDigestBytes > 0 {
			keep := a.opts.HistoryPruneKeepRecent
			if keep <= 0 {
				keep = defaultHistoryPruneKeepRecent
			}
			history = pruneRetroactiveToolHistory(history, a.opts.ToolDigestBytes, keep)
		}

		// Compaction: if history is getting large, summarise it before the next LLM call.
		// Fires only at the top of the loop so history is always in a consistent state
		// (no orphaned tool_calls without tool_results).
		//
		// H15 in audit ledger: convergence guard. The previous loop kept calling
		// compactHistory every step when the summary itself still exceeded the
		// threshold — each step burning an LLM call to produce a slightly larger
		// summary-of-summary until MaxSteps tripped. We now (a) refuse to use a
		// "compacted" result that didn't actually shrink history by at least 20%,
		// and (b) fall back to plain truncateMessages when compaction declines
		// to converge, breaking the loop on the very next step.
		if a.opts.CompactThresholdPct > 0 && a.opts.MaxPromptBytes > 0 {
			threshold := a.opts.MaxPromptBytes * a.opts.CompactThresholdPct / 100
			if historyBytes(history) > threshold {
				before := historyBytes(history)
				compacted, compactErr := a.compactHistory(ctx, userQuery, history)
				if compactErr != nil {
					a.logf("compaction failed (non-fatal), continuing with truncation: %v", compactErr)
					history = truncateMessages(history, a.opts.MaxPromptBytes)
				} else {
					after := historyBytes(compacted)
					if after*5 >= before*4 { // < 20% shrink
						a.logf("compaction did not converge: %d → %d bytes (≥80%% retained); falling back to truncation", before, after)
						history = truncateMessages(history, a.opts.MaxPromptBytes)
					} else {
						a.logf("history compacted: %d bytes → %d bytes", before, after)
						history = compacted
						cb.ResetReadOnlyCalls()
					}
				}
			}
		}

		// Inject a step-limit warning as a synthetic assistant message once at 2/3 of MaxSteps.
		if !maxStepsReminderSent && steps*3 >= a.opts.MaxSteps*2 {
			maxStepsReminderSent = true
			history = append(history, llm.Message{
				Role:    llm.RoleAssistant,
				Content: promptpkg.MaxStepsReminder,
			})
		}

		step, raw, llmResp, err := a.nextStep(ctx, userQuery, history, steps)
		if err != nil {
			return nil, nil, err
		}
		if step == nil {
			return nil, nil, fmt.Errorf("nextStep returned nil step without error")
		}
		// resp can be nil only if there was an error, which should have been returned above
		// But we handle it gracefully for tool calls that need tool_call_id

		switch step.Type {
		case StepToolCall:
			calls := a.resolveToolCalls(step, llmResp)
			if len(calls) == 0 {
				if cbErr := cb.RecordInvalid(); cbErr != nil {
					return nil, nil, cbErr
				}
				history = append(history, llm.Message{
					Role:    llm.RoleUser,
					Content: formatValidatorError("Invalid JSON format: tool is required", raw),
				})
				emitStepDone("invalid")
				continue
			}

			toolDefs := a.buildToolDefs()
			if len(calls) >= 2 && allParallelSafeCalls(calls, toolDefs) {
				var cbErr *protocol.Error
				history, cbErr = a.runParallelToolBatch(ctx, cb, history, calls, llmResp, steps)
				if cbErr != nil {
					return nil, nil, cbErr
				}
				emitStepDone("tool_call")
				continue
			}

			hasToolCalls := llmResp != nil && len(llmResp.Message.ToolCalls) > 0
			if hasToolCalls {
				history = append(history, llm.Message{
					Role:      llm.RoleAssistant,
					Content:   "",
					ToolCalls: llmResp.Message.ToolCalls,
				})
				a.logf("agent.tool_call added assistant message to history, history_len=%d, tool_calls=%d", len(history), len(calls))
			} else {
				a.logf("agent.tool_call WARNING: no tool_calls in response, history_len=%d", len(history))
			}

			for _, tc := range calls {
				outcome, err := a.runSerialToolCall(ctx, cb, &history, tc, steps, emitStepDone)
				if err != nil {
					return nil, nil, err
				}
				if outcome.EarlyResult != nil {
					return history, outcome.EarlyResult, nil
				}
			}
			emitStepDone("tool_call")
			continue

		case StepFinal:
			if step.Final == nil {
				history = append(history, llm.Message{
					Role:    llm.RoleUser,
					Content: formatValidatorError("Invalid JSON format: final is required", raw),
				})
				emitStepDone("invalid")
				continue
			}

			finalPatches := append([]patches.Patch{}, step.Final.Patches...)
			var internalOps []ops.AnyOp
			var applyResp *tools.FSApplyOpsResponse

			if !a.opts.Apply {
				// ── DRY-RUN PATH ─────────────────────────────────────────────────────────
				// Apply final.patches to the staging overlay (if any), then collect staged ops.
				if len(finalPatches) > 0 {
					a.logf("final received patches=%d → applying to staging overlay", len(finalPatches))
					start := time.Now()
					if err := a.tools.ApplyPatchesToStaged(finalPatches); err != nil {
						resolveMS := time.Since(start).Milliseconds()
						a.logf("staged-apply status=error duration_ms=%d err=%v", resolveMS, err)
						history = append(history, llm.Message{
							Role:    llm.RoleUser,
							Content: formatResolveErrorCompact(err),
						})
						if cbErr := cb.RecordFinalFailure(err); cbErr != nil {
							return nil, nil, cbErr
						}
						if a.opts.OnEvent != nil {
							var msg string
							if pe, ok := protocol.AsError(err); ok {
								msg = fmt.Sprintf("%s: %s", pe.Code, pe.Message)
							} else {
								msg = "staged-apply error: " + err.Error()
							}
							a.opts.OnEvent(AgentEvent{Step: steps, Stream: llm.StreamEvent{
								Kind:    llm.StreamEventRecoverableError,
								Content: msg,
							}})
						}
						emitStepDone("final_retry")
						continue
					}
				}

				stagedOps := a.tools.StagedOps()
				if len(stagedOps) == 0 {
					a.logf("final: no staged ops (no changes needed)")
					if llmResp != nil {
						history = append(history, llmResp.Message)
					}
					emitStepDone("final")
					return history, &Result{
						Steps:   steps,
						Patches: finalPatches,
						Applied: false,
						Todos:   a.todos,
					}, nil
				}
				internalOps = stagedOps
				a.logf("final staged_ops=%d → FSApplyOps dry_run=true", len(internalOps))

				start := time.Now()
				resp, err := a.tools.FSApplyOps(ctx, tools.FSApplyOpsRequest{
					Ops:    internalOps,
					DryRun: true,
					Backup: false,
				})
				applyMS := time.Since(start).Milliseconds()
				if err != nil {
					if pe, ok := protocol.AsError(err); ok && (pe.Code == protocol.StaleContent || pe.Code == protocol.AmbiguousMatch) {
						a.logf("staged-fsapply status=recoverable_error duration_ms=%d err=%v", applyMS, err)
						history = append(history, llm.Message{
							Role:    llm.RoleUser,
							Content: formatApplyErrorCompact(err, pe.Code),
						})
						if cbErr := cb.RecordFinalFailure(err); cbErr != nil {
							return nil, nil, cbErr
						}
						if a.opts.OnEvent != nil {
							a.opts.OnEvent(AgentEvent{Step: steps, Stream: llm.StreamEvent{
								Kind:    llm.StreamEventRecoverableError,
								Content: fmt.Sprintf("%s: %s", pe.Code, pe.Message),
							}})
						}
						emitStepDone("final_retry")
						continue
					}
					a.logf("staged-fsapply status=error duration_ms=%d err=%v", applyMS, err)
					return nil, nil, err
				}
				a.logf("staged-apply status=ok duration_ms=%d diffs=%d", applyMS, len(resp.Diffs))
				applyResp = resp

			} else {
				// ── APPLY PATH ───────────────────────────────────────────────────────────
				// Existing behavior: resolve patches → FSApplyOps with DryRun=false.
				if len(finalPatches) == 0 {
					a.logf("final received empty patches (no changes needed)")
					if llmResp != nil {
						history = append(history, llmResp.Message)
					}
					emitStepDone("final")
					return history, &Result{
						Steps:   steps,
						Patches: []patches.Patch{},
						Applied: false,
						Todos:   a.todos,
					}, nil
				}

				a.logf("final received patches=%d", len(finalPatches))
				start := time.Now()
				resolvedOps, err := resolver.ResolveExternalPatches(a.tools.WorkspaceRoot(), finalPatches)
				resolveMS := time.Since(start).Milliseconds()
				if err != nil {
					a.logf("resolve status=error duration_ms=%d err=%v", resolveMS, err)
					history = append(history, llm.Message{
						Role:    llm.RoleUser,
						Content: formatResolveErrorCompact(err),
					})
					if cbErr := cb.RecordFinalFailure(err); cbErr != nil {
						return nil, nil, cbErr
					}
					if a.opts.OnEvent != nil {
						var resolveMsg string
						if pe, ok := protocol.AsError(err); ok {
							resolveMsg = fmt.Sprintf("%s: %s", pe.Code, pe.Message)
						} else {
							resolveMsg = "resolve error: " + err.Error()
						}
						a.opts.OnEvent(AgentEvent{Step: steps, Stream: llm.StreamEvent{
							Kind:    llm.StreamEventRecoverableError,
							Content: resolveMsg,
						}})
					}
					emitStepDone("final_retry")
					continue
				}
				a.logf("resolve status=ok duration_ms=%d ops=%d", resolveMS, len(resolvedOps))

				start = time.Now()
				resp, err := a.tools.FSApplyOps(ctx, tools.FSApplyOpsRequest{
					Ops:    resolvedOps,
					DryRun: false,
					Backup: a.opts.Backup && a.opts.Apply,
				})
				applyMS := time.Since(start).Milliseconds()
				if err != nil {
					if pe, ok := protocol.AsError(err); ok && (pe.Code == protocol.StaleContent || pe.Code == protocol.AmbiguousMatch) {
						a.logf("apply status=recoverable_error duration_ms=%d err=%v", applyMS, err)
						history = append(history, llm.Message{
							Role:    llm.RoleUser,
							Content: formatApplyErrorCompact(err, pe.Code),
						})
						if cbErr := cb.RecordFinalFailure(err); cbErr != nil {
							return nil, nil, cbErr
						}
						if a.opts.OnEvent != nil {
							a.opts.OnEvent(AgentEvent{Step: steps, Stream: llm.StreamEvent{
								Kind:    llm.StreamEventRecoverableError,
								Content: fmt.Sprintf("%s: %s", pe.Code, pe.Message),
							}})
						}
						emitStepDone("final_retry")
						continue
					}
					a.logf("apply status=error duration_ms=%d err=%v", applyMS, err)
					return nil, nil, err
				}
				a.logf("apply status=ok duration_ms=%d diffs=%d", applyMS, len(resp.Diffs))
				internalOps = resolvedOps
				applyResp = resp
			}

			// ── COMMON TAIL ──────────────────────────────────────────────────────────
			if llmResp != nil {
				history = append(history, llmResp.Message)
			}
			if a.opts.OnEvent != nil && applyResp != nil {
				payload := map[string]any{
					"ops":     internalOps,
					"diff":    applyResp.Diffs,
					"applied": a.opts.Apply,
				}
				payloadJSON, _ := json.Marshal(payload)
				a.opts.OnEvent(AgentEvent{Step: steps, Stream: llm.StreamEvent{
					Kind:    llm.StreamEventPendingOps,
					Content: string(payloadJSON),
				}})
			}
			emitStepDone("final")
			return history, &Result{
				Steps:         steps,
				Patches:       finalPatches,
				Ops:           internalOps,
				Applied:       a.opts.Apply,
				ApplyResponse: applyResp,
				Todos:         a.todos,
			}, nil

		default:
			// M7+M8 in audit ledger: unknown step type counts toward the
			// MaxInvalidRetries cap so a model emitting persistently bogus
			// step shapes can't loop forever within MaxSteps.
			if cbErr := cb.RecordInvalid(); cbErr != nil {
				return nil, nil, cbErr
			}
			history = append(history, llm.Message{
				Role:    llm.RoleUser,
				Content: formatValidatorError("Invalid JSON format: unknown step type", raw),
			})
			emitStepDone("invalid")
		}
	}

	if res, ok := a.finalizeOnMaxSteps(ctx, history, steps); ok {
		return history, res, nil
	}
	return nil, nil, protocol.NewError(protocol.InvalidLLMOutput, "max_steps exceeded", map[string]any{
		"max_steps": a.opts.MaxSteps,
	})
}

// finalizeOnMaxSteps flushes staged changes when the step budget is exhausted
// so dry-run/preview runs do not lose write/edit progress made via tools.
func (a *Agent) finalizeOnMaxSteps(ctx context.Context, history []llm.Message, steps int) (*Result, bool) {
	stagedOps := a.tools.StagedOps()
	if len(stagedOps) == 0 {
		return nil, false
	}
	a.logf("max_steps: flushing %d staged op(s) before exit", len(stagedOps))
	resp, err := a.tools.FSApplyOps(ctx, tools.FSApplyOpsRequest{
		Ops:    stagedOps,
		DryRun: !a.opts.Apply,
		Backup: a.opts.Backup && a.opts.Apply,
	})
	if err != nil {
		a.logf("max_steps staged flush failed: %v", err)
		return nil, false
	}
	return &Result{
		Steps:            steps,
		Ops:              stagedOps,
		Applied:          a.opts.Apply,
		ApplyResponse:    resp,
		Todos:            a.todos,
		MaxStepsExceeded: true,
	}, true
}

// buildToolDefs returns the tool surface advertised to the LLM.
//
// Composition (H3 in architecture audit): CustomTools, when set, is the
// authoritative BASE — child agents use it to lock the surface down to a
// known read-only set. ExtraTools (MCP servers, plugin-provided tools)
// and the skill_invoke tool are ALWAYS appended on top so they remain
// available to child agents and skill-using agents. Previously
// CustomTools short-circuited the function and ExtraTools were silently
// dropped, making MCP invisible to any child agent.
//
// The result is memoised via toolDefsOnce because every input is fixed
// at agent construction (mode, capability flags, skills, subtasks).
// P1 in audit ledger (Sprint 6).
func (a *Agent) buildToolDefs() []llm.ToolDef {
	a.toolDefsOnce.Do(func() {
		a.toolDefsCache = a.computeToolDefs()
	})
	return a.toolDefsCache
}

func (a *Agent) computeToolDefs() []llm.ToolDef {
	var base []llm.ToolDef
	if len(a.opts.CustomTools) > 0 {
		// CustomTools replaces the mode-derived base; ExtraTools / skills
		// are still appended below.
		base = append(base, a.opts.CustomTools...)
	} else {
		caps := tools.Capabilities{
			Exec:    a.opts.AllowExec || len(a.opts.ExecAllow) > 0,
			Web:     a.opts.AllowWeb,
			Browser: a.opts.AllowBrowser,
		}
		hasSubtasks := a.opts.SubtaskRunner != nil
		hasQA := a.opts.QuestionAsker != nil
		switch {
		case a.opts.Mode != "":
			base = tools.ListToolsForMode(string(a.opts.Mode), caps, hasSubtasks, hasQA)
		case hasSubtasks:
			base = tools.ListToolsWithSubtasks(caps)
		default:
			base = tools.ListTools(caps)
		}
	}
	if len(a.opts.ExtraTools) > 0 {
		base = append(base, a.opts.ExtraTools...)
	}
	if a.opts.SkillRunner != nil && len(a.opts.Skills) > 0 {
		names := make([]string, len(a.opts.Skills))
		for i, s := range a.opts.Skills {
			names[i] = s.Name
		}
		base = append(base, tools.ToolSkillInvoke(names))
	}
	if a.opts.Mode == ModePlan || strings.TrimSpace(a.opts.PlanPath) != "" {
		for i := range base {
			base[i].Function.Description = a.substitutePlanPath(base[i].Function.Description)
		}
	}
	// Fast profile: drop LSP / browser tools unless CustomTools already fixed the set.
	if strings.EqualFold(a.opts.Profile, ProfileFast) && len(a.opts.CustomTools) == 0 {
		base = filterFastProfileTools(base)
	}
	return base
}

func filterFastProfileTools(in []llm.ToolDef) []llm.ToolDef {
	out := make([]llm.ToolDef, 0, len(in))
	for _, t := range in {
		name := t.Function.Name
		if strings.HasPrefix(name, "lsp.") || strings.HasPrefix(name, "browser.") {
			continue
		}
		out = append(out, t)
	}
	return out
}

func (a *Agent) effectivePlanPath() string {
	if p := strings.TrimSpace(a.opts.PlanPath); p != "" {
		return plan.NormalizeRelPath(p)
	}
	return plan.NormalizeRelPath(".orchestra/plan.md")
}

func (a *Agent) substitutePlanPath(s string) string {
	return strings.ReplaceAll(s, "{{PLAN_PATH}}", a.effectivePlanPath())
}

// buildSystemPrompt assembles the system message handed to the LLM each
// step. The pipeline has five distinct stages — the first that fires
// becomes the *base*, the rest *append* on top:
//
//  1. BASE candidate: promptpkg.BuildSystemPromptForMode(Mode, PromptFamily)
//     — the built-in prompt for the agent's mode.
//  2. BASE override: Options.SystemPromptOverride
//     — when a custom agent declares a system_prompt in .orchestra.yml,
//     it REPLACES the mode default.
//  3. BASE override (highest precedence): .orchestra/system.txt in the
//     workspace root, loaded via promptpkg.LoadSystemOverride. If this
//     file exists it REPLACES whatever was selected above, including the
//     custom-agent prompt — file-system wins over config.
//  4. APPEND: project memory (ORCHESTRA.md + .orchestra/memory/*.md +
//     ~/.orchestra/memory.md + optional session layer) via internal/memory.
//  5. APPEND: <available_tools> catalog from live tool defs (names + short desc).
//  6. APPEND: the <available_skills> block (when a SkillRunner is wired).
//
// M10 in architecture audit: this used to live as five ad-hoc if-checks
// inline in nextStep. The replace-vs-append asymmetry (1/2/3 replace,
// 4/5/6 append) was implicit and easy to mis-order on edit. Moving it to
// a method documents the contract and centralises the order so a new
// prompt source can be added in one place.
func (a *Agent) buildSystemPrompt() string {
	// 1+2+3: base — first non-empty replacement wins (.orchestra/system.txt
	// > Options.SystemPromptOverride > mode default).
	prompt := promptpkg.BuildSystemPromptForMode(string(a.opts.Mode), a.opts.PromptFamily)
	if a.opts.SystemPromptOverride != "" {
		prompt = a.opts.SystemPromptOverride
	}
	if fs := promptpkg.LoadSystemOverride(a.tools.WorkspaceRoot()); fs != "" {
		prompt = fs
	}
	// 4: append project memory (tiered, config-driven).
	memCfg := a.opts.Memory
	memCfg.Normalize()
	store := memory.NewStore(a.tools.WorkspaceRoot(), a.opts.SessionID, memCfg)
	if block := store.FormatInject(memCfg.InjectBytes()); block != "" {
		prompt += "\n\n" + block
	}
	// 5: live tool catalog (mode/caps accurate — better than hardcoded lists in *.txt).
	prompt += formatToolsCatalog(a.buildToolDefs())
	// 6: append skills advertisement.
	prompt += a.skillsAdvertisement()
	if a.opts.Mode == ModePlan || strings.TrimSpace(a.opts.PlanPath) != "" {
		prompt = a.substitutePlanPath(prompt)
	}
	return prompt
}

// skillsAdvertisement returns a system-prompt block describing the
// skills available to the model. Empty when no skills are configured
// or no SkillRunner is wired.
func (a *Agent) skillsAdvertisement() string {
	if a.opts.SkillRunner == nil || len(a.opts.Skills) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n<available_skills>\n")
	b.WriteString("You can invoke a skill via the skill_invoke tool when a subtask matches one. Pass {skill: <name>, task: <description>}; the result is returned synchronously.\n")
	for _, s := range a.opts.Skills {
		fmt.Fprintf(&b, "- %s — %s\n", s.Name, s.Description)
	}
	b.WriteString("</available_skills>")
	return b.String()
}

// nextStep returns the next step, raw response, full LLM response, and error.
// stepNum is the current step count (used for streaming event tagging).
func (a *Agent) nextStep(ctx context.Context, userQuery string, history []llm.Message, stepNum int) (*Step, string, *llm.CompleteResponse, error) {
	toolDefs := a.buildToolDefs()
	systemPrompt := a.buildSystemPrompt()
	snap := promptpkg.BuildUserInfoSnapshot(a.tools.WorkspaceRoot())
	userPrompt := promptpkg.BuildUserPrompt(userQuery, snap, tools.ToolNames(toolDefs))
	if block := renderTodosBlock(a.todos); block != "" {
		userPrompt = block + "\n" + userPrompt
	}
	// CKG context only on step 1 (saves tokens; later steps use explore/grep in history).
	if stepNum == 1 && a.ckgContext != "" {
		userPrompt += "\n\n" + a.ckgContext
	}
	// Mode reminder injected last (freshest in attention window).
	if reminder := a.modeReminder(); reminder != "" {
		userPrompt += "\n\n" + reminder
	}

	// Build messages: system + user (initial) + history (assistant tool calls + tool results)
	messages := make([]llm.Message, 0, len(history)+2)
	messages = append(messages, llm.Message{
		Role:    llm.RoleSystem,
		Content: systemPrompt,
	})
	if len(a.opts.UserImages) > 0 {
		parts := make([]llm.ContentPart, 0, 1+len(a.opts.UserImages))
		parts = append(parts, llm.ContentPart{Kind: llm.PartText, Text: userPrompt})
		parts = append(parts, a.opts.UserImages...)
		messages = append(messages, llm.Message{Role: llm.RoleUser, Parts: parts})
	} else {
		messages = append(messages, llm.Message{
			Role:    llm.RoleUser,
			Content: userPrompt,
		})
	}
	messages = append(messages, history...)

	// Debug: log history length before truncation
	if a.opts.Debug {
		a.logf("agent.nextStep history_len=%d messages_before_truncate=%d", len(history), len(messages))
	}

	// Truncate messages if needed to stay within budget (best-effort)
	if a.opts.MaxPromptBytes > 0 {
		beforeTruncate := len(messages)
		messages = truncateMessages(messages, a.opts.MaxPromptBytes)
		if a.opts.Debug && len(messages) != beforeTruncate {
			a.logf("agent.nextStep messages truncated: %d -> %d (budget=%d)", beforeTruncate, len(messages), a.opts.MaxPromptBytes)
		}
	}

	if a.opts.Debug {
		totalBytes := 0
		for _, m := range messages {
			totalBytes += m.TextLen()
		}
		// Build roles string for debug logging
		roles := make([]string, 0, len(messages))
		for _, m := range messages {
			roleStr := string(m.Role)
			if m.Role == llm.RoleAssistant && len(m.ToolCalls) > 0 {
				roleStr = fmt.Sprintf("%s(tool_calls=%d)", roleStr, len(m.ToolCalls))
			}
			if m.Role == llm.RoleTool && m.ToolCallID != "" {
				roleStr = fmt.Sprintf("%s(id=%s)", roleStr, truncateID(m.ToolCallID, 12))
			}
			roles = append(roles, roleStr)
		}
		a.logf("agent.step messages_count=%d roles=%v total_bytes=%d tools=%d", len(messages), roles, totalBytes, len(toolDefs))
	}

	var lastInvalid *protocol.Error
	var lastRaw string

	// Determine streaming availability once; steps is captured in closure below.
	streamer, canStream := a.llm.(llm.Streamer)

	for attempt := 0; attempt <= a.opts.MaxInvalidRetries; attempt++ {
		stepCtx := ctx
		var cancel context.CancelFunc
		if a.opts.LLMStepTimeout > 0 {
			stepCtx, cancel = context.WithTimeout(ctx, a.opts.LLMStepTimeout)
		}
		llmReq := llm.CompleteRequest{
			Messages:       messages,
			Tools:          toolDefs,
			ResponseFormat: a.opts.ResponseFormat,
		}
		var resp *llm.CompleteResponse
		var err error
		if canStream && a.opts.OnEvent != nil {
			resp, err = a.streamStep(stepCtx, llmReq, streamer, stepNum)
		} else {
			resp, err = a.llm.Complete(stepCtx, llmReq)
		}
		if cancel != nil {
			cancel() // Always cancel timeout context to free resources
		}
		if err != nil {
			return nil, "", nil, err
		}
		if a.opts.UsageTracker != nil && resp != nil {
			if resp.Usage != nil {
				a.opts.UsageTracker.Record(a.opts.ProviderLabel, a.opts.ModelLabel,
					resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
			} else {
				// M4 in audit ledger: log when the provider returned no
				// usage payload so usage.jsonl silently understating tokens
				// is at least visible to the operator. Common with local
				// LLM proxies and some streaming back-ends.
				a.logf("usage: provider %q returned nil Usage; this run undercounts tokens for one step", a.opts.ProviderLabel)
			}
		}
		step, raw, nerr := NormalizeLLMWithDefs(a.validator, resp, toolDefs)
		lastRaw = raw

		if nerr != nil {
			// Inject validation error as user message and retry
			if pe, ok := protocol.AsError(nerr); ok {
				lastInvalid = pe
			} else {
				lastInvalid = protocol.NewError(protocol.InvalidLLMOutput, nerr.Error(), nil)
			}
			// Add error feedback to messages for retry
			errorMsg := llm.Message{
				Role:    llm.RoleUser,
				Content: formatValidatorErrorCompact(lastInvalid.Message),
			}
			messages = append(messages, errorMsg)
			// Truncate again if needed
			if a.opts.MaxPromptBytes > 0 {
				messages = truncateMessages(messages, a.opts.MaxPromptBytes)
			}
			if a.opts.OnEvent != nil {
				msg := "schema invalid: " + lastInvalid.Message
				msg = truncate(msg, 200)
				a.opts.OnEvent(AgentEvent{Step: stepNum, Stream: llm.StreamEvent{
					Kind:    llm.StreamEventRecoverableError,
					Content: msg,
				}})
			}
			continue
		}

		// Note: exec.run policy validation is handled in Run() after adding assistant message to history.
		// This allows proper tool calling loop with tool messages.

		return step, raw, resp, nil
	}

	if lastInvalid != nil {
		return nil, lastRaw, nil, lastInvalid
	}
	return nil, lastRaw, nil, protocol.NewError(protocol.InvalidLLMOutput, "Invalid JSON format: unknown validation failure", nil)
}

// streamStep calls CompleteStream and forwards events to OnEvent, returning the
// final assembled CompleteResponse from the Done event.
func (a *Agent) streamStep(ctx context.Context, req llm.CompleteRequest, s llm.Streamer, step int) (*llm.CompleteResponse, error) {
	ch, err := s.CompleteStream(ctx, req)
	if err != nil {
		return nil, err
	}
	var final *llm.CompleteResponse
	for ev := range ch {
		if a.opts.OnEvent != nil {
			a.opts.OnEvent(AgentEvent{Step: step, Stream: ev})
		}
		switch ev.Kind {
		case llm.StreamEventError:
			return nil, ev.Err
		case llm.StreamEventDone:
			final = ev.Response
		}
	}
	if final == nil {
		return nil, fmt.Errorf("stream ended without Done event")
	}
	return final, nil
}

func (a *Agent) logf(format string, args ...any) {
	if !a.opts.Debug {
		return
	}
	if a.opts.Logger != nil {
		a.opts.Logger.Printf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// handleTodoTool handles todo.read and todo.write in-process (no runner involvement).
func (a *Agent) handleTodoTool(name string, input json.RawMessage) (json.RawMessage, error) {
	switch name {
	case "todowrite":
		var req tools.TodoWriteRequest
		if err := json.Unmarshal(input, &req); err != nil {
			return nil, fmt.Errorf("todo.write: invalid input: %w", err)
		}
		normalized, err := tools.ValidateTodos(req.Todos)
		if err != nil {
			return nil, fmt.Errorf("todo.write: %w", err)
		}
		a.todos = normalized
		resp, _ := json.Marshal(tools.TodoWriteResponse{Count: len(normalized)})
		return resp, nil
	case "todoread":
		resp, _ := json.Marshal(tools.TodoReadResponse{Todos: a.todos})
		return resp, nil
	default:
		return nil, fmt.Errorf("unknown todo tool: %s", name)
	}
}

// renderTodosBlock returns a formatted todo block for injection into the user prompt.
// Returns empty string when todos is empty.
func renderTodosBlock(todos []tools.TodoItem) string {
	if len(todos) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<todo_list>\n")
	for _, item := range todos {
		b.WriteString(fmt.Sprintf("- [%s] %s (id: %s)\n", item.Status, item.Content, item.ID))
	}
	b.WriteString("</todo_list>\n")
	return b.String()
}

// handleSkillInvoke handles skill_invoke in-process via SkillRunner.
// Validates the requested skill name against Options.Skills before running.
func (a *Agent) handleSkillInvoke(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Skill string `json:"skill"`
		Task  string `json:"task"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, fmt.Errorf("skill_invoke: invalid input: %w", err)
	}
	if strings.TrimSpace(req.Skill) == "" {
		return nil, fmt.Errorf("skill_invoke: skill is required")
	}
	if strings.TrimSpace(req.Task) == "" {
		return nil, fmt.Errorf("skill_invoke: task is required")
	}
	known := false
	for _, s := range a.opts.Skills {
		if s.Name == req.Skill {
			known = true
			break
		}
	}
	if !known {
		return nil, fmt.Errorf("skill_invoke: unknown skill %q", req.Skill)
	}
	result, err := a.opts.SkillRunner.InvokeSkill(ctx, req.Skill, req.Task)
	if err != nil {
		return nil, fmt.Errorf("skill_invoke: %w", err)
	}
	resp, _ := json.Marshal(map[string]any{
		"skill":  req.Skill,
		"status": "done",
		"result": result,
	})
	return resp, nil
}

// handleTaskTool handles task / task.spawn / task.wait / task.cancel in-process via SubtaskRunner.
func (a *Agent) handleTaskTool(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	switch name {
	case "task":
		var req struct {
			Description  string `json:"description"`
			Prompt       string `json:"prompt"`
			Goal         string `json:"goal"`
			SubagentType string `json:"subagent_type"`
			MaxSteps     int    `json:"max_steps"`
			TimeoutMS    int    `json:"timeout_ms"`
		}
		if err := json.Unmarshal(input, &req); err != nil {
			return nil, fmt.Errorf("task: invalid input: %w", err)
		}
		goal := strings.TrimSpace(req.Prompt)
		if goal == "" {
			goal = strings.TrimSpace(req.Goal)
		}
		if goal == "" {
			return nil, fmt.Errorf("task: prompt is required")
		}
		subagentType := strings.TrimSpace(req.SubagentType)
		if subagentType == "" {
			subagentType = "explore"
		}
		timeoutMS := req.TimeoutMS
		if timeoutMS <= 0 {
			timeoutMS = 120_000
		}
		taskID, err := a.opts.SubtaskRunner.Spawn(ctx, SubtaskSpawnRequest{
			Goal:         goal,
			SubagentType: subagentType,
			MaxSteps:     req.MaxSteps,
			TimeoutMS:    timeoutMS,
		})
		if err != nil {
			return nil, fmt.Errorf("task: spawn: %w", err)
		}
		result, err := a.opts.SubtaskRunner.Wait(ctx, taskID, timeoutMS)
		if err != nil {
			return nil, fmt.Errorf("task: wait: %w", err)
		}
		out := map[string]any{
			"task_id": taskID,
			"status":  result.Status,
		}
		if req.Description != "" {
			out["description"] = req.Description
		}
		if result.Result != "" {
			out["result"] = result.Result
		}
		if result.Error != "" {
			out["error"] = result.Error
		}
		resp, _ := json.Marshal(out)
		return resp, nil

	case "task_spawn":
		var req struct {
			Goal         string `json:"goal"`
			Prompt       string `json:"prompt"`
			SubagentType string `json:"subagent_type"`
			MaxSteps     int    `json:"max_steps"`
			TimeoutMS    int    `json:"timeout_ms"`
		}
		if err := json.Unmarshal(input, &req); err != nil {
			return nil, fmt.Errorf("task.spawn: invalid input: %w", err)
		}
		goal := strings.TrimSpace(req.Goal)
		if goal == "" {
			goal = strings.TrimSpace(req.Prompt)
		}
		if goal == "" {
			return nil, fmt.Errorf("task.spawn: goal is required")
		}
		subagentType := strings.TrimSpace(req.SubagentType)
		if subagentType == "" {
			subagentType = "explore"
		}
		taskID, err := a.opts.SubtaskRunner.Spawn(ctx, SubtaskSpawnRequest{
			Goal:         goal,
			SubagentType: subagentType,
			MaxSteps:     req.MaxSteps,
			TimeoutMS:    req.TimeoutMS,
		})
		if err != nil {
			return nil, fmt.Errorf("task.spawn: %w", err)
		}
		resp, _ := json.Marshal(map[string]any{"task_id": taskID, "status": "spawned"})
		return resp, nil

	case "task_wait":
		var req struct {
			TaskID    string `json:"task_id"`
			TimeoutMS int    `json:"timeout_ms"`
		}
		if err := json.Unmarshal(input, &req); err != nil {
			return nil, fmt.Errorf("task.wait: invalid input: %w", err)
		}
		if strings.TrimSpace(req.TaskID) == "" {
			return nil, fmt.Errorf("task.wait: task_id is required")
		}
		result, err := a.opts.SubtaskRunner.Wait(ctx, req.TaskID, req.TimeoutMS)
		if err != nil {
			return nil, fmt.Errorf("task.wait: %w", err)
		}
		resp, _ := json.Marshal(result)
		return resp, nil

	case "task_cancel":
		var req struct {
			TaskID string `json:"task_id"`
		}
		if err := json.Unmarshal(input, &req); err != nil {
			return nil, fmt.Errorf("task.cancel: invalid input: %w", err)
		}
		if strings.TrimSpace(req.TaskID) == "" {
			return nil, fmt.Errorf("task.cancel: task_id is required")
		}
		if err := a.opts.SubtaskRunner.Cancel(ctx, req.TaskID); err != nil {
			return nil, fmt.Errorf("task.cancel: %w", err)
		}
		resp, _ := json.Marshal(map[string]any{"task_id": req.TaskID, "status": "cancelled"})
		return resp, nil

	default:
		return nil, fmt.Errorf("unknown task tool: %s", name)
	}
}

// modeReminder returns the reminder string to append to the user prompt for the current mode.
// The build-switch reminder fires at most once (cleared after the first call).
func (a *Agent) modeReminder() string {
	switch a.opts.Mode {
	case ModePlan:
		return a.substitutePlanPath(promptpkg.PlanModeReminder)
	case ModeBuild, "":
		if a.justSwitchedFromPlan {
			a.justSwitchedFromPlan = false
			return a.substitutePlanPath(promptpkg.BuildSwitchReminder)
		}
	}
	return ""
}

// execCommandFromInput extracts the command basename from exec.run JSON input.
func execCommandFromInput(input json.RawMessage) string {
	var req struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(input, &req)
	return req.Command
}

// execCommandAllowed reports whether cmd is permitted by the allow/deny lists.
// Deny takes precedence. Empty allow list with no deny list → deny all.
func execCommandAllowed(cmd string, allow, deny []string) bool {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(cmd)))
	base = strings.TrimSuffix(base, ".exe")
	if base == "" || base == "." {
		return false
	}
	for _, d := range deny {
		if strings.ToLower(strings.TrimSpace(d)) == base {
			return false
		}
	}
	if len(allow) == 0 {
		return false
	}
	for _, a := range allow {
		if strings.ToLower(strings.TrimSpace(a)) == base {
			return true
		}
	}
	return false
}

// formatToolDeniedJSON formats a tool denial as JSON for tool message content.
func formatToolDeniedJSON(name string, input json.RawMessage, reason string) string {
	result := map[string]any{
		"status": "denied",
		"tool":   name,
		"reason": reason,
		"input":  compactJSON(input),
	}
	b, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf(`{"status":"denied","tool":"%s","reason":"%s"}`, name, reason)
	}
	return string(b)
}

// formatToolErrorJSON formats a tool error as JSON for tool message content.
func formatToolErrorJSON(name string, input json.RawMessage, err error) string {
	result := map[string]any{
		"status": "error",
		"tool":   name,
		"input":  compactJSON(input),
	}
	if pe, ok := protocol.AsError(err); ok {
		result["code"] = string(pe.Code)
		result["error"] = pe.Message
	} else {
		result["error"] = err.Error()
	}
	b, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf(`{"status":"error","tool":"%s","error":"%s"}`, name, err.Error())
	}
	return string(b)
}

// parallelBatchWorkerLimit caps simultaneous goroutines fanned out from a
// single parallel tool batch. Sized to keep file-handle and LSP-query
// pressure reasonable on big batches while still finishing well below the
// per-step LLM timeout for typical read-heavy turns.
const parallelBatchWorkerLimit = 16

// runParallelToolBatch executes a batch of read-only tool calls concurrently
// (capped by parallelBatchWorkerLimit) and appends the assistant + tool
// messages to history in their original order. The OpenAI tool-calling
// protocol requires that every tool_call_id from the assistant message has
// exactly one matching tool message reply, so we preserve ordering even when
// individual calls finish out-of-order.
//
// Only ParallelSafe tools reach this path — see NormalizeLLMWithDefs. The
// rich per-tool serial pipeline (exec consent, plan-mode write guard, todo
// dispatcher, …) is therefore not exercised here; we go straight through
// PreTool hook + audit log + a.tools.Call.
//
// Hook safety: PreTool hooks run SERIALLY before the parallel fan-out. Most
// real-world hooks aren't thread-safe (they append to log files, mutate
// external state, or take 0.5-1s of process start-up that compounds badly
// under 16-way concurrency). The actual tool execution (file I/O) is where
// the latency is, so parallelism there is what matters.
// runParallelToolBatch returns the updated history AND a non-nil *protocol.Error
// when one of the circuit breakers tripped during result bookkeeping
// (e.g. parallel batch of 16 denied tools blows MaxDeniedToolRepeats).
// The caller must propagate the error out of Run, exactly like the serial
// path does. C2 in docs/superpowers/plans/2026-05-19-post-audit-refactor.md.
func (a *Agent) runParallelToolBatch(ctx context.Context, cb *CircuitBreaker, history []llm.Message, calls []ToolCall, llmResp *llm.CompleteResponse, stepNum int) ([]llm.Message, *protocol.Error) {
	// 1) Append the assistant message that requested the batch. OpenAI's
	//    protocol requires the assistant message with tool_calls to precede
	//    the per-tool replies; reuse the original ToolCalls slice unchanged.
	if llmResp != nil {
		history = append(history, llm.Message{
			Role:      llm.RoleAssistant,
			Content:   "",
			ToolCalls: llmResp.Message.ToolCalls,
		})
	}

	results := make([]string, len(calls))
	// denied[i] is true when the pre-tool hook rejected call i — we'll skip
	// the parallel tool execution for that index and just emit the denial.
	denied := make([]bool, len(calls))
	// errored[i] is true when a.tools.Call returned a non-nil error for call i.
	// Used after wg.Wait to drive circuit-breaker bookkeeping in order.
	errored := make([]bool, len(calls))

	// 2) PreTool hooks: run serially. Most hooks aren't reentrant (they spawn
	//    a subprocess that writes to a shared log file). Spawning 16 of them
	//    concurrently turns each call into a 5-second timeout because of
	//    file-lock contention and OS process-startup pressure.
	if a.opts.HooksRunner != nil {
		for i, call := range calls {
			hookErr := safeRunErr("PreTool hook "+call.Name, func() error {
				return a.opts.HooksRunner.RunPreTool(ctx, call.Name, call.Input)
			})
			if hookErr != nil {
				denied[i] = true
				results[i] = formatToolDeniedJSON(call.Name, call.Input, "pre-tool hook denied: "+hookErr.Error())
				if a.opts.OnEvent != nil {
					_ = safeRun("OnEvent ToolCallCompleted (pre-deny)", func() {
						a.opts.OnEvent(AgentEvent{Step: stepNum, Stream: llm.StreamEvent{
							Kind:         llm.StreamEventToolCallCompleted,
							ToolCallID:   call.ID,
							ToolCallName: call.Name,
							Content:      "error: pre-tool hook denied",
						}})
					})
				}
			}
		}
	}

	// 3) Fan out tool execution. Results are collected by index so the tool
	//    reply order matches the assistant message's tool_calls order.
	sem := make(chan struct{}, parallelBatchWorkerLimit)
	var wg sync.WaitGroup

	for i, tc := range calls {
		if denied[i] {
			continue
		}
		if dedupExemptTool(tc.Name) && cb != nil && cb.IsReadOnlyBlocked(tc.Name, tc.Input) {
			denied[i] = true
			results[i] = "⛔ STOP. The tool «" + tc.Name + "» was called too many times with identical arguments — proceed with a different tool or final answer."
			continue
		}
		wg.Add(1)
		go func(idx int, call ToolCall) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Worker is wrapped in recover so a panicking tool implementation
			// kills only this one call, not the whole process. A.Run sees the
			// synthesised error via results[idx] and the circuit breaker
			// counts it like any other tool error.
			err := safeRunErr("parallel tool "+call.Name, func() error {
				if a.opts.AgentLogger != nil {
					a.opts.AgentLogger.LogToolCall(call.Name, len(call.Input))
				}
				out, callErr := a.tools.Call(ctx, call.Name, call.Input)
				if callErr != nil {
					return callErr
				}
				results[idx] = a.prepareToolHistoryContent(call.Name, call.Input, out)
				if a.opts.OnEvent != nil {
					// OnEvent in its own recover so a buggy UI sink doesn't take
					// down the worker mid-success. Recovered value is dropped —
					// next event will retry.
					_ = safeRun("OnEvent ToolCallCompleted", func() {
						a.opts.OnEvent(AgentEvent{Step: stepNum, Stream: llm.StreamEvent{
							Kind:         llm.StreamEventToolCallCompleted,
							ToolCallID:   call.ID,
							ToolCallName: call.Name,
							Content:      truncate(string(out), 256),
						}})
					})
				}
				return nil
			})
			if err != nil {
				errored[idx] = true
				results[idx] = formatToolErrorJSON(call.Name, call.Input, err)
				if a.opts.OnEvent != nil {
					_ = safeRun("OnEvent ToolCallCompleted (err)", func() {
						a.opts.OnEvent(AgentEvent{Step: stepNum, Stream: llm.StreamEvent{
							Kind:         llm.StreamEventToolCallCompleted,
							ToolCallID:   call.ID,
							ToolCallName: call.Name,
							Content:      "error: " + truncate(err.Error(), 200),
						}})
					})
				}
			}
		}(i, tc)
	}
	wg.Wait()

	// 4) Stitch tool replies into history in original order.
	for i, tc := range calls {
		history = append(history, llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: tc.ID,
			Content:    results[i],
		})
	}

	// 5) Circuit-breaker bookkeeping — runs single-threaded after fan-out so
	//    the unsynchronised CB counters stay consistent. Order matches the
	//    original calls slice so a stable "first trip wins" decision is taken.
	//    The model spamming 16 parallel denied/erroring tools must stop the
	//    run on the same step it stopped on serially. Mirrors the serial
	//    pipeline at agent.go:483-686.
	if cb != nil {
		anyErr := false
		var readOnlyHints []string
		for i, call := range calls {
			switch {
			case denied[i]:
				if cbErr := cb.RecordDenied(call.Name); cbErr != nil {
					return history, cbErr
				}
			case errored[i]:
				anyErr = true
				if cbErr := cb.RecordToolError(call.Name); cbErr != nil {
					return history, cbErr
				}
			default:
				if dedupExemptTool(call.Name) {
					if hint := cb.RecordReadOnlyCall(call.Name, call.Input); hint != "" {
						readOnlyHints = append(readOnlyHints, hint)
					}
				} else {
					_ = cb.RecordSuccessfulCall(call.Name, call.Input)
				}
				cb.ResetDeniedForTool(call.Name)
			}
		}
		if !anyErr && len(calls) > 0 {
			cb.ResetToolErrors()
		}
		for _, hint := range readOnlyHints {
			history = append(history, llm.Message{Role: llm.RoleUser, Content: hint})
		}
	}
	return history, nil
}
