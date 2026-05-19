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
	promptpkg "github.com/orchestra/orchestra/internal/prompt"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/resolver"
	"github.com/orchestra/orchestra/internal/schema"
	"github.com/orchestra/orchestra/internal/tools"

	"github.com/orchestra/orchestra/internal/llm"
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
	Goal      string
	MaxSteps  int
	TimeoutMS int
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

// Agent mode constants.
const (
	ModeBuild   = "build"   // default: full tool access
	ModePlan    = "plan"    // read-only + plan tools
	ModeExplore = "explore" // grep/glob/read only (subagent)

	// ModeGeneral is a multi-step execution subagent: full read+write tools, returns via task_result.
	ModeGeneral = "general"
	// ModeCompaction is an internal agent that compresses conversation history into a summary.
	ModeCompaction = "compaction"
	// ModeTitle is an internal agent that generates a short task title from the user query.
	ModeTitle = "title"
	// ModeSummary is an internal agent that produces a brief summary of completed work.
	ModeSummary = "summary"
)

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

	// Mode selects the agent role: "build" (default), "plan" (read-only), "explore" (subagent).
	// Empty string behaves identically to "build" for backward compatibility.
	Mode string

	// QuestionAsker, if non-nil, enables the question tool (interactive user Q&A).
	// Use StdinQuestionAsker for direct CLI mode. Must be nil for orchestra core (stdio conflict).
	QuestionAsker tools.QuestionAsker

	// JustSwitchedFromPlan, when true, injects a one-shot build-switch reminder on the first step.
	// Set by the caller when restarting an agent in build mode after plan approval.
	JustSwitchedFromPlan bool

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
	// Pre-fetch relevant CKG nodes once per Run; injected into every nextStep prompt.
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
			// Parallel batch fast-path. Set when NormalizeLLM saw multiple
			// tool_calls and every one of them is registry-flagged ParallelSafe.
			// We bypass the rich serial pipeline (per-tool permissions, exec
			// auth, todo/task dispatcher, …) because read-only tools never hit
			// those code paths anyway — only the generic tools.Call + the
			// PreTool hook + audit log. See runParallelToolBatch.
			if len(step.Tools) >= 2 {
				var cbErr *protocol.Error
				history, cbErr = a.runParallelToolBatch(ctx, cb, history, step.Tools, llmResp, steps)
				if cbErr != nil {
					return nil, nil, cbErr
				}
				emitStepDone("tool_call")
				continue
			}

			if step.Tool == nil {
				// M7+M8 in audit ledger: count invalid steps toward the
				// MaxInvalidRetries cap via RecordInvalid (was dead code).
				// Without this, a model stuck emitting malformed JSON could
				// only be bounded by MaxSteps.
				if cbErr := cb.RecordInvalid(); cbErr != nil {
					return nil, nil, cbErr
				}
				// Add validation error as user message for retry
				history = append(history, llm.Message{
					Role:    llm.RoleUser,
					Content: formatValidatorError("Invalid JSON format: tool is required", raw),
				})
				emitStepDone("invalid")
				continue
			}
			name := strings.TrimSpace(step.Tool.Name)
			if name == "" {
				if cbErr := cb.RecordInvalid(); cbErr != nil {
					return nil, nil, cbErr
				}
				history = append(history, llm.Message{
					Role:    llm.RoleUser,
					Content: formatValidatorError("Invalid JSON format: tool.name is empty", raw),
				})
				emitStepDone("invalid")
				continue
			}
			// Normalize LLM-facing aliases (read, bash, edit, todowrite, task_result, …)
			// to canonical names so every downstream name check (exec consent,
			// Extract tool_call_id from response
			toolCallID := ""
			hasToolCalls := llmResp != nil && len(llmResp.Message.ToolCalls) > 0
			if hasToolCalls {
				toolCallID = llmResp.Message.ToolCalls[0].ID
			}
			// Serial-fallback cleanup: when the LLM emitted several parallel
			// tool_calls but the batch contained a Mutating tool, NormalizeLLM
			// kept only the first call (Step.Tool) and dropped the rest. The
			// SSE stream already pushed tool_call_start events for every entry,
			// so the TUI now has orphan running blocks for the dropped ones.
			// Emit synthetic "skipped" completions so they don't stay stuck.
			if hasToolCalls && len(llmResp.Message.ToolCalls) > 1 && a.opts.OnEvent != nil {
				for _, extra := range llmResp.Message.ToolCalls[1:] {
					a.opts.OnEvent(AgentEvent{Step: steps, Stream: llm.StreamEvent{
						Kind:         llm.StreamEventToolCallCompleted,
						ToolCallID:   extra.ID,
						ToolCallName: extra.Function.Name,
						Content:      "skipped: only the first tool call in a mixed batch is executed",
					}})
				}
			}
			if toolCallID == "" {
				// Fallback: generate a synthetic ID if not provided (for legacy JSON format)
				toolCallID = fmt.Sprintf("call_%d_%d", steps, time.Now().UnixNano())
			}

			// Add assistant message with tool_calls (required for proper tool calling loop)
			// Only add if we have actual tool_calls (not legacy JSON format in content)
			if hasToolCalls {
				// When tool_calls are present, content should be empty (some providers don't like content + tool_calls)
				assistantMsg := llm.Message{
					Role:      llm.RoleAssistant,
					Content:   "", // Clear content when tool_calls are present
					ToolCalls: llmResp.Message.ToolCalls,
				}
				history = append(history, assistantMsg)
				a.logf("agent.tool_call added assistant message to history, history_len=%d, tool_call_id=%s", len(history), toolCallID)
			} else {
				a.logf("agent.tool_call WARNING: no tool_calls in response, history_len=%d", len(history))
			}

			// Permission ruleset: evaluated first; first matching rule wins.
			// allow → bypasses the AllowExec/AllowWeb consent gates for this call only.
			// deny  → TOOL_DENIED regardless of --allow-exec/--allow-web.
			effectiveAllowExec := a.opts.AllowExec
			effectiveAllowWeb := a.opts.AllowWeb
			if len(a.opts.PermissionRules) > 0 {
				subject := subjectForTool(name, step.Tool.Input)
				if act, matched := checkPermissions(a.opts.PermissionRules, name, subject); matched {
					if act == "deny" {
						toolResult := formatToolDeniedJSON(name, step.Tool.Input, "tool call denied by permission ruleset")
						history = append(history, llm.Message{
							Role:       llm.RoleTool,
							ToolCallID: toolCallID,
							Content:    toolResult,
						})
						if cbErr := cb.RecordDenied(name); cbErr != nil {
							return nil, nil, cbErr
						}
						emitStepDone("tool_call")
						continue
					}
					// act == "allow": grant consent for this call only.
					effectiveAllowExec = true
					effectiveAllowWeb = true
				}
			}

			// Interactive consent via PermissionRequester (TUI/IDE): checked before the static gate.
			// Approved → effectiveAllowExec = true (static gate becomes a no-op for this call).
			// Denied  → TOOL_DENIED using the same pathway as the static gate below.
			// Error   → fall through to static gate (defensive).
			if name == "bash" && !effectiveAllowExec && a.opts.PermissionRequester != nil {
				cmdPreview := ""
				if len(step.Tool.Input) > 0 {
					cmdPreview = string(step.Tool.Input)
					if len(cmdPreview) > 200 {
						cmdPreview = cmdPreview[:200] + "..."
					}
				}
				resp, permErr := a.opts.PermissionRequester.RequestPermission(ctx, PermissionRequest{
					Tool:        "bash",
					Description: cmdPreview,
				})
				if permErr == nil && resp.Approved {
					effectiveAllowExec = true
				} else if permErr == nil && !resp.Approved {
					reason := "exec.run denied by interactive permission requester"
					if resp.Reason != "" {
						reason = resp.Reason
					}
					toolResult := formatToolDeniedJSON(name, step.Tool.Input, reason)
					history = append(history, llm.Message{
						Role:       llm.RoleTool,
						ToolCallID: toolCallID,
						Content:    toolResult,
					})
					if cbErr := cb.RecordDenied(name); cbErr != nil {
						return nil, nil, cbErr
					}
					emitStepDone("tool_call")
					continue
				}
				// permErr != nil: fall through to static gate.
			}

			// Consent policy: block exec.run unless AllowExec (all allowed) or per-command allowlist permits it.
			if name == "bash" && !effectiveAllowExec {
				cmd := execCommandFromInput(step.Tool.Input)
				if !execCommandAllowed(cmd, a.opts.ExecAllow, a.opts.ExecDeny) {
					msg := "exec.run requires user consent (use --allow-exec or configure exec.allow)"
					if len(a.opts.ExecAllow) > 0 {
						msg = fmt.Sprintf("exec.run: command %q is not in the allowlist", cmd)
					}
					toolResult := formatToolDeniedJSON(name, step.Tool.Input, msg)
					history = append(history, llm.Message{
						Role:       llm.RoleTool,
						ToolCallID: toolCallID,
						Content:    toolResult,
					})
					if cbErr := cb.RecordDenied(name); cbErr != nil {
						return nil, nil, cbErr
					}
					emitStepDone("tool_call")
					continue
				}
			}

			// Consent policy: block webfetch unless AllowWeb.
			if name == "webfetch" && !effectiveAllowWeb {
				toolResult := formatToolDeniedJSON(name, step.Tool.Input, "webfetch requires user consent (use --allow-web)")
				history = append(history, llm.Message{
					Role:       llm.RoleTool,
					ToolCallID: toolCallID,
					Content:    toolResult,
				})
				if cbErr := cb.RecordDenied(name); cbErr != nil {
					return nil, nil, cbErr
				}
				emitStepDone("tool_call")
				continue
			}

			// task.result: child agent reports its answer and exits immediately.
			if name == "task_result" {
				// H11 in audit ledger: task_result terminates the run with
				// the supplied string. That's only valid for child agents
				// (subtask / skill spawn). A main agent emitting task_result
				// is a confused model trying to short-circuit — instead of
				// terminating, push a hint back so it produces a normal
				// final response on the next step.
				if !a.opts.IsChild {
					history = append(history, llm.Message{
						Role:       llm.RoleTool,
						ToolCallID: toolCallID,
						Content:    formatToolErrorJSON(name, step.Tool.Input, fmt.Errorf("task_result is only valid in subtask / skill_invoke child agents; main agents must emit a normal final response with patches")),
					})
					if cbErr := cb.RecordToolError(name); cbErr != nil {
						return nil, nil, cbErr
					}
					emitStepDone("tool_call")
					continue
				}
				var req struct {
					Content string `json:"content"`
				}
				_ = json.Unmarshal(step.Tool.Input, &req)
				emitStepDone("final")
				return history, &Result{
					Steps:         steps,
					SubtaskResult: req.Content,
					Todos:         a.todos,
				}, nil
			}

			// skill_invoke is handled in-process via SkillRunner (synchronous).
			if a.opts.SkillRunner != nil && name == "skill_invoke" {
				out, skillErr := a.handleSkillInvoke(ctx, step.Tool.Input)
				var content string
				if skillErr != nil {
					content = formatToolErrorJSON(name, step.Tool.Input, skillErr)
				} else {
					content = string(out)
				}
				history = append(history, llm.Message{
					Role:       llm.RoleTool,
					ToolCallID: toolCallID,
					Content:    content,
				})
				if skillErr != nil {
					if cbErr := cb.RecordToolError(name); cbErr != nil {
						return nil, nil, cbErr
					}
				} else {
					cb.ResetToolErrors()
				}
				emitStepDone("tool_call")
				continue
			}

			// task.spawn/wait/cancel are handled in-process via SubtaskRunner.
			if a.opts.SubtaskRunner != nil && (name == "task_spawn" || name == "task_wait" || name == "task_cancel") {
				out, taskErr := a.handleTaskTool(ctx, name, step.Tool.Input)
				var content string
				if taskErr != nil {
					content = formatToolErrorJSON(name, step.Tool.Input, taskErr)
				} else {
					content = string(out)
				}
				history = append(history, llm.Message{
					Role:       llm.RoleTool,
					ToolCallID: toolCallID,
					Content:    content,
				})
				if taskErr != nil {
					if cbErr := cb.RecordToolError(name); cbErr != nil {
						return nil, nil, cbErr
					}
				} else {
					cb.ResetToolErrors()
				}
				emitStepDone("tool_call")
				continue
			}

			// todo.write / todo.read are handled in-process (session state, no filesystem access).
			if name == "todowrite" || name == "todoread" {
				out, err := a.handleTodoTool(name, step.Tool.Input)
				var content string
				if err != nil {
					content = formatToolErrorJSON(name, step.Tool.Input, err)
				} else {
					content = string(out)
				}
				history = append(history, llm.Message{
					Role:       llm.RoleTool,
					ToolCallID: toolCallID,
					Content:    content,
				})
				if err != nil {
					if cbErr := cb.RecordToolError(name); cbErr != nil {
						return nil, nil, cbErr
					}
				} else {
					cb.ResetToolErrors()
				}
				emitStepDone("tool_call")
				continue
			}

			// question: block until user answers via QuestionAsker.
			if name == "question" {
				var req struct {
					Questions []tools.QuestionItem `json:"questions"`
				}
				if qErr := json.Unmarshal(step.Tool.Input, &req); qErr != nil || a.opts.QuestionAsker == nil {
					msg := `{"error":"question tool unavailable"}`
					if a.opts.QuestionAsker != nil {
						msg = formatToolErrorJSON(name, step.Tool.Input, qErr)
					}
					history = append(history, llm.Message{Role: llm.RoleTool, ToolCallID: toolCallID, Content: msg})
					if cbErr := cb.RecordToolError(name); cbErr != nil {
						return nil, nil, cbErr
					}
					emitStepDone("tool_call")
					continue
				}
				answers, qErr := a.opts.QuestionAsker.Ask(ctx, req.Questions)
				var content string
				if qErr != nil {
					content = formatToolErrorJSON(name, step.Tool.Input, qErr)
					if cbErr := cb.RecordToolError(name); cbErr != nil {
						return nil, nil, cbErr
					}
				} else {
					b, _ := json.Marshal(map[string]any{"answers": answers})
					content = string(b)
					cb.ResetToolErrors()
				}
				history = append(history, llm.Message{Role: llm.RoleTool, ToolCallID: toolCallID, Content: content})
				emitStepDone("tool_call")
				continue
			}

			// plan_exit: ask user approval, then signal mode switch or continue planning.
			if name == "plan_exit" {
				approved := false
				if a.opts.QuestionAsker != nil {
					answers, qErr := a.opts.QuestionAsker.Ask(ctx, []tools.QuestionItem{{
						Question: "План готов. Переключиться в режим build для применения изменений?",
						Options:  []string{"Да, переключить в build", "Нет, продолжить планирование"},
					}})
					if qErr == nil && len(answers) > 0 {
						ans := strings.ToLower(strings.TrimSpace(answers[0]))
						approved = ans == "1" || ans == "да" || ans == "yes" || strings.HasPrefix(ans, "да,")
					}
				} else {
					// L2 in audit ledger: when no QuestionAsker is available we
					// CANNOT silently flip to build mode — the caller asked for
					// plan mode and expects to stay there. Refuse the mode
					// switch and tell the model so it returns a normal final
					// answer with the plan instead.
					history = append(history, llm.Message{
						Role:       llm.RoleTool,
						ToolCallID: toolCallID,
						Content:    `{"status":"refused","message":"plan_exit недоступен в non-interactive режиме. Заверши шаг финальным ответом — пользователь сам переключит режим, если нужно."}`,
					})
					emitStepDone("tool_call")
					continue
				}
				if approved {
					emitStepDone("final")
					return history, &Result{Steps: steps, SwitchToBuild: true, Todos: a.todos}, nil
				}
				history = append(history, llm.Message{
					Role:       llm.RoleTool,
					ToolCallID: toolCallID,
					Content:    `{"status":"continue","message":"Продолжаем планирование. Доработай план и вызови plan_exit снова."}`,
				})
				emitStepDone("tool_call")
				continue
			}

			// plan_enter: stub — switching modes in-process is not supported yet.
			if name == "plan_enter" {
				history = append(history, llm.Message{
					Role:       llm.RoleTool,
					ToolCallID: toolCallID,
					Content:    `{"status":"not_supported","message":"plan_enter недоступен в текущем режиме. Запусти orchestra apply --mode plan для планирования."}`,
				})
				emitStepDone("tool_call")
				continue
			}

			// Plan-mode write guard: only .orchestra/plan.md writes are allowed.
			if a.opts.Mode == ModePlan && (name == "write" || name == "edit") {
				var pathReq struct {
					Path string `json:"path"`
				}
				allowed := false
				if json.Unmarshal(step.Tool.Input, &pathReq) == nil {
					p := filepath.ToSlash(filepath.Clean(strings.TrimSpace(pathReq.Path)))
					allowed = p == ".orchestra/plan.md"
				}
				if !allowed {
					toolResult := formatToolDeniedJSON(name, step.Tool.Input, "план-режим: запись разрешена только в .orchestra/plan.md")
					history = append(history, llm.Message{Role: llm.RoleTool, ToolCallID: toolCallID, Content: toolResult})
					if cbErr := cb.RecordDenied(name); cbErr != nil {
						return nil, nil, cbErr
					}
					emitStepDone("tool_call")
					continue
				}
			}

			// For exec.run with streaming enabled, forward output chunks via OnEvent.
			callCtx := ctx
			if name == "bash" && a.opts.OnEvent != nil {
				capturedStep := steps
				onEvent := a.opts.OnEvent
				callCtx = tools.WithExecOutputCallback(ctx, func(chunk string) {
					onEvent(AgentEvent{Step: capturedStep, Stream: llm.StreamEvent{
						Kind:    llm.StreamEventExecOutput,
						Content: chunk,
					}})
				})
			}

			// Pre-tool hook: non-zero exit denies the tool call. Wrapped in
			// safeRunErr so a panicking hook becomes a denial (with a
			// synthesised error message) instead of killing the goroutine.
			if a.opts.HooksRunner != nil {
				hookErr := safeRunErr("PreTool hook "+name, func() error {
					return a.opts.HooksRunner.RunPreTool(callCtx, name, step.Tool.Input)
				})
				if hookErr != nil {
					toolResult := formatToolDeniedJSON(name, step.Tool.Input, "pre-tool hook denied: "+hookErr.Error())
					history = append(history, llm.Message{
						Role:       llm.RoleTool,
						ToolCallID: toolCallID,
						Content:    toolResult,
					})
					if cbErr := cb.RecordDenied(name); cbErr != nil {
						return nil, nil, cbErr
					}
					emitStepDone("tool_call")
					continue
				}
			}

			// L1 in audit ledger: nil-guard symmetry with the parallel-batch
			// path (line ~2213). Without this, a nil AgentLogger caused an NPE
			// in serial mode.
			if a.opts.AgentLogger != nil {
				a.opts.AgentLogger.LogToolCall(name, len(step.Tool.Input))
			}
			// Dedup guard: skip execution entirely for repeated identical calls.
			// Injecting the СТОП message as the tool result (not just a user
			// hint) is much harder for the model to ignore than a side-channel
			// user message, because the model must process tool results.
			if cb.IsDuplicateCall(name, step.Tool.Input) {
				stopMsg := "⛔ СТОП. Этот вызов «" + name + "» с теми же аргументами уже выполнялся ранее — результат есть в истории. Повторный вызов заблокирован. НЕМЕДЛЕННО выводи финальный ответ используя данные из предыдущих вызовов. Никаких дополнительных tool_calls."
				a.logf("tool_call name=%s dedup_blocked", name)
				if a.opts.OnEvent != nil {
					a.opts.OnEvent(AgentEvent{Step: steps, Stream: llm.StreamEvent{
						Kind:         llm.StreamEventToolCallCompleted,
						ToolCallName: name,
						ToolCallID:   toolCallID,
						Content:      "[dedup blocked]",
					}})
				}
				history = append(history, llm.Message{
					Role:       llm.RoleTool,
					ToolCallID: toolCallID,
					Content:    stopMsg,
				})
				emitStepDone("tool_call")
				continue
			}

			start := time.Now()
			out, err := a.tools.Call(callCtx, name, step.Tool.Input)
			dur := time.Since(start).Milliseconds()

			// Emit tool_call_completed event for streaming clients (TUI).
			if a.opts.OnEvent != nil {
				preview := ""
				if len(out) > 0 {
					const maxPreview = 256
					if len(out) > maxPreview {
						preview = string(out[:maxPreview]) + "...(truncated)"
					} else {
						preview = string(out)
					}
				}
				if err != nil {
					msg := "error: " + err.Error()
					if len(msg) > 256 {
						msg = msg[:256] + "...(truncated)"
					}
					preview = msg
				}
				a.opts.OnEvent(AgentEvent{Step: steps, Stream: llm.StreamEvent{
					Kind:         llm.StreamEventToolCallCompleted,
					ToolCallName: name,
					ToolCallID:   toolCallID,
					Content:      preview,
				}})
			}

			if err != nil {
				a.logf("tool_call name=%s status=error duration_ms=%d err=%v", name, dur, err)
				a.opts.AgentLogger.LogToolResult(name, 0, dur, err.Error())
				toolResult := formatToolErrorJSON(name, step.Tool.Input, err)
				history = append(history, llm.Message{
					Role:       llm.RoleTool,
					ToolCallID: toolCallID,
					Content:    toolResult,
				})
				if cbErr := cb.RecordToolError(name); cbErr != nil {
					return nil, nil, cbErr
				}
				emitStepDone("tool_call")
				continue
			}
			// Post-tool hook: errors logged but do not fail the tool. Panic
			// converted to error by safeRunErr so a flaky logger hook can't
			// take down the agent loop.
			if a.opts.HooksRunner != nil {
				_ = safeRunErr("PostTool hook "+name, func() error {
					a.opts.HooksRunner.RunPostTool(callCtx, name, out)
					return nil
				})
			}
			a.logf("tool_call name=%s status=ok duration_ms=%d output_bytes=%d", name, dur, len(out))
			a.opts.AgentLogger.LogToolResult(name, len(out), dur, "")
			history = append(history, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: toolCallID,
				Content:    string(out),
			})
			// Multimodal pipe: browser.screenshot returns base64 PNG. When the
			// configured LLM is multimodal, inject a synthetic user message
			// carrying the image as a PartImage so the model can "see" it on
			// the next step (tool result alone is just a JSON-wrapped base64
			// string the model can't decode without explicit support).
			if a.opts.MultimodalLLM && name == "browser.screenshot" {
				if part, ok := extractScreenshotImagePart(out); ok {
					history = append(history, llm.Message{
						Role: llm.RoleUser,
						Parts: []llm.ContentPart{
							{Kind: llm.PartText, Text: "Screenshot returned by browser.screenshot:"},
							part,
						},
					})
				}
			}
			// Inject LSP error hint so the model fixes compile errors in the next step.
			if name == "write" || name == "edit" {
				if hint := extractLSPErrors(out); hint != "" {
					a.logf("lsp_hint name=%s injecting diagnostic hint", name)
					history = append(history, llm.Message{
						Role:    llm.RoleUser,
						Content: hint,
					})
					if a.opts.OnEvent != nil {
						a.opts.OnEvent(AgentEvent{Step: steps, Stream: llm.StreamEvent{
							Kind:    llm.StreamEventRecoverableError,
							Content: "lsp_errors: " + name,
						}})
					}
				}
			}
			// Record call for future dedup checks. N6 (audit ledger,
			// Sprint 6): RecordSuccessfulCall returns a strong "you already
			// did this" hint when the same (tool, args) succeeds twice.
			// Inject it as a user message so the model sees an imperative
			// nudge before it can call the same tool again — previously
			// this return value was discarded, leaving only the next-step
			// dedup tool-result block (which catches a 3rd repeat but
			// silently accepts the 2nd).
			if dupHint := cb.RecordSuccessfulCall(name, step.Tool.Input); dupHint != "" {
				history = append(history, llm.Message{Role: llm.RoleUser, Content: dupHint})
			}
			a.logf("agent.tool_call added tool message to history, history_len=%d, tool_call_id=%s", len(history), toolCallID)
			cb.ResetToolErrors()
			// N3 (audit ledger, Sprint 6): clear stale denial counter for
			// this tool — a successful call means whatever was blocking it
			// (permission rule, missing capability flag) is gone.
			cb.ResetDeniedForTool(name)
			// M6 in audit ledger: a successful tool call resets the final-
			// failure counter on the rationale that the model is making
			// progress between apply attempts. This makes MaxFinalFailures
			// "consecutive failures with no intervening tool success" rather
			// than "lifetime failures". Trade-off: a model in a
			// fail→read→fail loop only trips on MaxSteps, not MaxFinalFailures.
			// Accept by design: MaxSteps caps every loop shape; this cap
			// targets a narrower "no progress at all" failure mode.
			cb.ResetFinalFailures()
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

	return nil, nil, protocol.NewError(protocol.InvalidLLMOutput, "max_steps exceeded", map[string]any{
		"max_steps": a.opts.MaxSteps,
	})
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
		allowExec := a.opts.AllowExec || len(a.opts.ExecAllow) > 0
		allowWeb := a.opts.AllowWeb
		allowBrowser := a.opts.AllowBrowser
		hasSubtasks := a.opts.SubtaskRunner != nil
		hasQA := a.opts.QuestionAsker != nil
		switch {
		case a.opts.Mode != "":
			base = tools.ListToolsForMode(a.opts.Mode, allowExec, allowWeb, allowBrowser, hasSubtasks, hasQA)
		case hasSubtasks:
			base = tools.ListToolsWithSubtasks(allowExec, allowWeb, allowBrowser)
		default:
			base = tools.ListTools(allowExec, allowWeb, allowBrowser)
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
	return base
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
	systemPrompt := promptpkg.BuildSystemPromptForMode(a.opts.Mode, a.opts.PromptFamily)
	// Custom agent system_prompt overrides the built-in mode prompt.
	if a.opts.SystemPromptOverride != "" {
		systemPrompt = a.opts.SystemPromptOverride
	}
	// .orchestra/system.txt in the workspace root overrides everything.
	if override := promptpkg.LoadSystemOverride(a.tools.WorkspaceRoot()); override != "" {
		systemPrompt = override
	}
	if memory := promptpkg.LoadProjectMemory(a.tools.WorkspaceRoot(), 2048); memory != "" {
		systemPrompt += "\n\n" + memory
	}
	systemPrompt += a.skillsAdvertisement()
	snap := promptpkg.BuildUserInfoSnapshot(a.tools.WorkspaceRoot())
	userPrompt := promptpkg.BuildUserPrompt(userQuery, snap, tools.ToolNames(toolDefs))
	if block := renderTodosBlock(a.todos); block != "" {
		userPrompt = block + "\n" + userPrompt
	}
	// CKG context appended at end: attention-bias to recent content.
	if a.ckgContext != "" {
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

func buildBasePrompt(userQuery string, allowExec bool) string {
	// Keep this prompt compact but explicit: JSON-only, step schema, allowed tools.
	toolsList := strings.TrimSpace(`
Доступные инструменты:
- fs.list
- fs.read
- search.text
- code.symbols
`)
	if allowExec {
		toolsList += "\n- exec.run"
	} else {
		toolsList += "\nВАЖНО: exec.run сейчас НЕДОСТУПЕН (не предлагай и не вызывай его)."
	}

	return strings.TrimSpace(fmt.Sprintf(`
Ты — агент для работы с кодовой базой в workspace. Твоя цель: выполнить задачу пользователя.

ВАЖНО:
- Ты ДОЛЖЕН отвечать ТОЛЬКО валидным JSON (без markdown, без пояснений).
- Каждый ответ — это ОДИН следующий шаг.

Формат шага (AgentStep):
1) Tool call:
{"type":"tool_call","tool":{"name":"ls","input":{...}}}

2) Final:
{"type":"final","final":{"patches":[ ... ]}}

%s

Патчи (final.patches) поддерживают только:
- {"type":"file.search_replace","path":"...","search":"...","replace":"...","file_hash":"sha256:..."}
- {"type":"file.unified_diff","path":"...","diff":"...","file_hash":"sha256:..."}

Правила:
- Для каждого файла, который ты меняешь, сначала сделай fs.read, чтобы получить точный file_hash.
- Не генерируй внутренние ops и не пытайся применять изменения инструментами. Верни изменения ТОЛЬКО через final.patches.

Задача пользователя:
%s
`, toolsList, userQuery))
}

func buildPromptWithHistory(base string, history []string, maxBytes int) string {
	if maxBytes <= 0 {
		return base + "\n"
	}
	header := base + "\n\nИстория (самые свежие события в конце):\n"
	footer := "\n\nВерни ТОЛЬКО JSON следующего шага.\n"

	// Keep the tail of history within the byte budget.
	budget := maxBytes - len(header) - len(footer)
	if budget <= 0 {
		// Worst case: return only base prompt.
		return base
	}

	var selected []string
	size := 0
	for i := len(history) - 1; i >= 0; i-- {
		item := history[i]
		need := len(item) + 2
		if size+need > budget {
			break
		}
		selected = append(selected, item)
		size += need
	}
	// Reverse back to chronological order.
	for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
		selected[i], selected[j] = selected[j], selected[i]
	}

	return header + strings.Join(selected, "\n\n") + footer
}

func formatToolOK(name string, input json.RawMessage, output json.RawMessage) string {
	return "TOOL_OK " + name + "\ninput=" + compactJSON(input) + "\noutput=" + compactJSON(output)
}

func formatToolError(name string, input json.RawMessage, err error) string {
	code := ""
	if pe, ok := protocol.AsError(err); ok {
		code = string(pe.Code)
	}
	if code != "" {
		return "TOOL_ERR " + name + " code=" + code + "\ninput=" + compactJSON(input) + "\nerror=" + formatErr(err)
	}
	return "TOOL_ERR " + name + "\ninput=" + compactJSON(input) + "\nerror=" + formatErr(err)
}

func formatToolDenied(name string, input json.RawMessage, reason string) string {
	return "TOOL_DENIED " + name + "\ninput=" + compactJSON(input) + "\nreason=" + reason
}

func formatValidatorError(msg string, raw string) string {
	return "VALIDATION_ERROR\nmessage=" + msg + "\nraw=" + truncate(strings.TrimSpace(raw), 400)
}

// formatValidatorErrorCompact returns a compact error message without raw JSON to avoid prompt bloat.
func formatValidatorErrorCompact(msg string) string {
	return "VALIDATION_ERROR\nmessage=" + msg + "\nИсправь формат JSON согласно схеме (tool call или PatchSet)."
}

// formatPolicyDeniedCompact returns a compact policy denial message.
func formatPolicyDeniedCompact(toolName string) string {
	return fmt.Sprintf("TOOL_DENIED %s\nreason=требует явного разрешения\nИспользуй только доступные инструменты из списка.", toolName)
}

func formatResolveError(err error) string {
	return "RESOLVE_ERROR\nerror=" + formatErr(err)
}

// errorDataString pulls a string field out of a protocol.Error's Data
// payload. Returns "" when the field is missing or not a string. Used by
// the compact error formatters to surface the resolver's structured
// context (path, matches count, hash) to the LLM.
func errorDataString(pe *protocol.Error, key string) string {
	if pe == nil || pe.Data == nil {
		return ""
	}
	m, ok := pe.Data.(map[string]any)
	if !ok {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func errorDataInt(pe *protocol.Error, key string) int {
	if pe == nil || pe.Data == nil {
		return 0
	}
	m, ok := pe.Data.(map[string]any)
	if !ok {
		return 0
	}
	switch v := m[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	}
	return 0
}

// errorDataInts pulls an []int field (possibly serialised as []float64
// after a JSON round-trip). Used for the AmbiguousMatch match_lines list.
func errorDataInts(pe *protocol.Error, key string) []int {
	if pe == nil || pe.Data == nil {
		return nil
	}
	m, ok := pe.Data.(map[string]any)
	if !ok {
		return nil
	}
	switch v := m[key].(type) {
	case []int:
		return v
	case []any:
		out := make([]int, 0, len(v))
		for _, x := range v {
			switch n := x.(type) {
			case int:
				out = append(out, n)
			case float64:
				out = append(out, int(n))
			}
		}
		return out
	}
	return nil
}

// formatResolveErrorCompact returns a compact resolve error message. H1
// fix: include path from the resolver's Data payload so the model knows
// which file to re-read instead of guessing across a multi-file patch.
// L3 in audit ledger: hints are English — most chat-tuned LLMs respond
// more reliably to English error contexts. The path/code tokens are
// the load-bearing structural fields.
func formatResolveErrorCompact(err error) string {
	if pe, ok := protocol.AsError(err); ok {
		path := errorDataString(pe, "path")
		if path != "" {
			return fmt.Sprintf("RESOLVE_ERROR code=%s path=%s\nRe-read the file (fs.read) and update file_hash in the patch.", pe.Code, path)
		}
		return fmt.Sprintf("RESOLVE_ERROR code=%s\nRe-read the file (fs.read) and update file_hash in the patch.", pe.Code)
	}
	return "RESOLVE_ERROR code=unknown\nerror=" + err.Error() + "\nRe-read the file (fs.read) and update file_hash in the patch."
}

func formatApplyError(err error) string {
	return "APPLY_ERROR\nerror=" + formatErr(err)
}

// formatApplyErrorCompact returns a compact apply error message with
// actionable hint. H1 fix (audit ledger): include path + matches count
// from the resolver's structured Data payload so the LLM gets to pinpoint
// the failing file in a multi-file patch and knows how many ambiguous
// hits it needs to disambiguate. Previously the hint was a single
// "файл изменился" line with zero specifics — the biggest single driver
// of retry-loop bloat in observed runs.
func formatApplyErrorCompact(err error, code protocol.ErrorCode) string {
	pe, _ := protocol.AsError(err)
	path := errorDataString(pe, "path")
	pathSuffix := ""
	if path != "" {
		pathSuffix = " path=" + path
	}
	switch code {
	case protocol.StaleContent:
		return "APPLY_ERROR code=StaleContent" + pathSuffix +
			"\nFile changed on disk. Re-read it (fs.read) and update the patch with the new file_hash."
	case protocol.AmbiguousMatch:
		matches := errorDataInt(pe, "matches")
		linesPart := ""
		if lines := errorDataInts(pe, "match_lines"); len(lines) > 0 {
			// M13 in audit ledger: surface first N match line numbers so
			// the LLM picks disambiguating context from the actual hits.
			parts := make([]string, 0, len(lines))
			for _, ln := range lines {
				parts = append(parts, fmt.Sprintf("%d", ln))
			}
			linesPart = " lines=" + strings.Join(parts, ",")
		}
		if matches > 0 {
			return fmt.Sprintf("APPLY_ERROR code=AmbiguousMatch%s matches=%d%s\nSearch block matched %d locations. Disambiguate: add 2-3 lines of context before or after the existing search.", pathSuffix, matches, linesPart, matches)
		}
		return "APPLY_ERROR code=AmbiguousMatch" + pathSuffix + linesPart +
			"\nSearch block is ambiguous. Add more surrounding context to make it unique."
	}
	return "APPLY_ERROR code=unknown\nerror=" + formatErr(err)
}

// maxLSPErrorsInjected caps how many diagnostics are pasted back into the
// agent's history after a write/edit. A syntax error that cascades into
// hundreds of parser errors (large generated TS file, broken Go go.mod)
// would otherwise blow MaxPromptBytes and force aggressive truncation —
// the model loses useful context and the diagnostics themselves still
// don't all fit. H2 in audit ledger.
const maxLSPErrorsInjected = 20

// extractLSPErrors parses a write/edit tool response JSON and returns a
// user-facing hint if diagnostics with severity "error" are present.
// Capped at maxLSPErrorsInjected entries — additional errors are
// summarised as "...N more" so the model knows the report is partial.
// Returns "" if there are no errors (warnings and info are silently ignored).
func extractLSPErrors(out json.RawMessage) string {
	if len(out) == 0 {
		return ""
	}
	var resp struct {
		Diagnostics []struct {
			Severity  string `json:"severity"`
			Message   string `json:"message"`
			StartLine int    `json:"start_line"`
			StartCol  int    `json:"start_col"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal(out, &resp); err != nil || len(resp.Diagnostics) == 0 {
		return ""
	}
	var errs []string
	total := 0
	for _, d := range resp.Diagnostics {
		if d.Severity != "error" {
			continue
		}
		total++
		if len(errs) < maxLSPErrorsInjected {
			errs = append(errs, fmt.Sprintf("  line %d:%d: %s", d.StartLine, d.StartCol, d.Message))
		}
	}
	if total == 0 {
		return ""
	}
	body := strings.Join(errs, "\n")
	if total > maxLSPErrorsInjected {
		body += fmt.Sprintf("\n  …и ещё %d ошибок (показаны первые %d)", total-maxLSPErrorsInjected, maxLSPErrorsInjected)
	}
	// N6 in audit ledger (Sprint 6): framed as a denial-style hint so the
	// model treats it as a constraint, not a side note. Identical re-call
	// is also blocked at the dedup gate (agent.go:885), but the soft form
	// here nudges the model to think before retrying with cosmetic tweaks.
	return "LSP_ERRORS — следующий write/edit на тот же файл с теми же ошибками будет заблокирован. Файл записан, но имеет ошибки компиляции:\n" +
		body +
		"\nДиагностируй причину (read + lsp.hover / lsp.references), исправь её и только затем — write/edit. Косметические правки тех же строк не помогут."
}

func formatErr(err error) string {
	if err == nil {
		return ""
	}
	if pe, ok := protocol.AsError(err); ok {
		b, _ := json.Marshal(pe)
		return string(b)
	}
	return err.Error()
}

func compactJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return truncate(string(raw), 400)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return truncate(string(raw), 400)
	}
	return string(b)
}

func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + " ...(truncated)"
}

// truncateID truncates an ID string for logging
func truncateID(id string, maxLen int) string {
	if len(id) <= maxLen {
		return id
	}
	return id[:maxLen] + "..."
}

// sanitizeOrphanedToolCalls returns a copy of msgs where every tool_call
// whose ID is in orphans is removed from the first message (the assistant
// opener of an atom). If that leaves the assistant with no tool_calls AND
// no other content, the assistant message is dropped entirely so we don't
// emit an empty turn to the LLM. Subsequent messages in msgs are returned
// unchanged.
//
// N4 in audit ledger (Sprint 6): without this, an assistant that opens
// three tool_calls but only receives two replies (network glitch, mid-
// batch error) would keep the orphan in history forever, hard-failing
// every subsequent LLM step with "tool_call_id not found".
func sanitizeOrphanedToolCalls(msgs []llm.Message, orphans map[string]bool) []llm.Message {
	if len(msgs) == 0 || len(orphans) == 0 {
		return msgs
	}
	head := msgs[0]
	if head.Role != llm.RoleAssistant || len(head.ToolCalls) == 0 {
		return msgs
	}
	kept := make([]llm.ToolCall, 0, len(head.ToolCalls))
	for _, tc := range head.ToolCalls {
		if !orphans[tc.ID] {
			kept = append(kept, tc)
		}
	}
	head.ToolCalls = kept
	out := make([]llm.Message, 0, len(msgs))
	if len(kept) > 0 || head.TextLen() > 0 || len(head.Parts) > 0 {
		out = append(out, head)
	}
	out = append(out, msgs[1:]...)
	return out
}

// truncateMessages truncates message history to fit within byte budget.
// Always keeps system and first user message, then keeps as many history
// messages as possible from the tail, preserving every assistant↔tool group
// as one atom.
//
// "Atom" semantics: an assistant message that carries `tool_calls` MUST stay
// together with every subsequent `tool` message whose `tool_call_id` matches
// one of those calls — splitting them produces an orphaned `tool` (or a
// dangling assistant whose calls have no replies), which OpenAI / Anthropic
// reject with "tool_call_id not found", hard-failing the next LLM call and
// killing the whole run. We therefore build atoms first, then greedy-pick
// the most recent atoms whole.
//
// This replaces an earlier per-message loop that could keep a lone `tool`
// message when its paired assistant exceeded the budget (C1 in the audit).
//
// Complexity: O(n) in the number of messages. estimateMessageSize is called
// exactly once per message during atom-build and the cached atom.size is
// reused by the greedy-pick. The sanitize path re-estimates only the atoms
// that had orphan tool_calls — typically zero. P2 in audit ledger (Sprint 6)
// flagged a suspected double estimate; on re-reading the code, the second
// pass it claimed doesn't exist.
func truncateMessages(messages []llm.Message, maxBytes int) []llm.Message {
	if maxBytes <= 0 || len(messages) <= 2 {
		return messages
	}

	// Always keep system (0) and first user (1)
	required := messages[:2]
	requiredSize := 0
	for _, m := range required {
		requiredSize += estimateMessageSize(m)
	}

	if requiredSize >= maxBytes {
		// Worst case: return only required messages.
		return required
	}

	budget := maxBytes - requiredSize

	// Walk forward from index 2 building atoms. An assistant with `ToolCalls`
	// opens a group; every following `tool` message whose `ToolCallID` matches
	// any open call joins the group; any other message (or a `tool` with no
	// matching open call — defensive: shouldn't happen in valid history)
	// closes the group and becomes its own atom.
	type atom struct {
		msgs []llm.Message
		size int
	}
	atoms := make([]atom, 0, len(messages)-2)
	// flush appends cur to atoms. If openCalls is non-empty when the atom
	// closes (some tool_call_ids never received a matching tool result —
	// partial API response, mid-batch failure), strip those orphaned IDs
	// from the assistant message so the surviving history stays self-
	// consistent. Otherwise the next LLM call would reject the prompt
	// with "tool_call_id not found". N4 in audit ledger (Sprint 6).
	flush := func(a *atom, openCalls map[string]bool) {
		if len(a.msgs) == 0 {
			return
		}
		if len(openCalls) > 0 {
			a.msgs = sanitizeOrphanedToolCalls(a.msgs, openCalls)
			a.size = 0
			for _, mm := range a.msgs {
				a.size += estimateMessageSize(mm)
			}
		}
		if len(a.msgs) > 0 {
			atoms = append(atoms, *a)
		}
	}
	cur := atom{}
	openCalls := map[string]bool{}
	for i := 2; i < len(messages); i++ {
		m := messages[i]
		ms := estimateMessageSize(m)
		switch {
		case m.Role == llm.RoleTool && m.ToolCallID != "" && openCalls[m.ToolCallID]:
			// Belongs to the in-progress assistant↔tool group.
			cur.msgs = append(cur.msgs, m)
			cur.size += ms
			delete(openCalls, m.ToolCallID)
		case m.Role == llm.RoleAssistant && len(m.ToolCalls) > 0:
			flush(&cur, openCalls)
			cur = atom{msgs: []llm.Message{m}, size: ms}
			openCalls = map[string]bool{}
			for _, tc := range m.ToolCalls {
				openCalls[tc.ID] = true
			}
		default:
			flush(&cur, openCalls)
			cur = atom{msgs: []llm.Message{m}, size: ms}
			openCalls = map[string]bool{}
		}
	}
	flush(&cur, openCalls)

	// Greedy-pick atoms from the tail forward until budget is exhausted.
	// We iterate backwards so the most recent context survives; the partial-
	// fit atom and everything before it are dropped wholesale (preserving the
	// "no orphaned tool" invariant).
	tailStart := len(atoms)
	size := 0
	for i := len(atoms) - 1; i >= 0; i-- {
		if size+atoms[i].size > budget {
			break
		}
		size += atoms[i].size
		tailStart = i
	}

	result := make([]llm.Message, 0, len(required)+len(messages)-2)
	result = append(result, required...)
	for i := tailStart; i < len(atoms); i++ {
		result = append(result, atoms[i].msgs...)
	}
	return result
}

// estimateMessageSize estimates the byte size of a message for truncation purposes.
// Accounts for Content/Parts text, ToolCalls (JSON serialization overhead), and ToolCallID.
// Image bytes are counted with a fixed per-image penalty rather than their raw
// base64 length — huge image payloads would otherwise dominate the budget and
// force compaction to evict useful tool results.
//
// M2 in audit ledger: the raw size estimate consistently undercounts real
// serialised JSON (per-tool-call overhead, role/content key names, escapes
// in strings). We apply a ×1.2 safety margin so the truncation budget
// stays below the real prompt size — overshooting MaxPromptBytes triggers
// LLM context-length errors that are hard to debug.
func estimateMessageSize(msg llm.Message) int {
	size := msg.TextLen()
	for _, p := range msg.Parts {
		if p.Kind == llm.PartImage {
			size += 4096 // fixed image budget per part
		}
	}
	if msg.ToolCallID != "" {
		// ToolCallID adds to JSON size (field name + value)
		size += len(msg.ToolCallID) + 20 // approximate overhead for "tool_call_id":"..."
	}
	if len(msg.ToolCalls) > 0 {
		// Estimate tool_calls size: each tool call has id, type, function.name, function.arguments
		for _, tc := range msg.ToolCalls {
			size += len(tc.ID) + len(tc.Type) + len(tc.Function.Name)
			// Arguments are already in Content or as separate field, but add overhead
			size += len(tc.Function.Arguments.Raw()) + 50 // JSON structure overhead
		}
	}
	// Safety margin: real JSON serialisation adds role keys, content keys,
	// escapes, and per-message envelope. Without this ×1.2, truncate's
	// "fits in budget" picks atoms that actually overflow on the wire.
	return size * 12 / 10
}

// handleTodoTool handles todo.read and todo.write in-process (no runner involvement).
func (a *Agent) handleTodoTool(name string, input json.RawMessage) (json.RawMessage, error) {
	switch name {
	case "todowrite":
		var req tools.TodoWriteRequest
		if err := json.Unmarshal(input, &req); err != nil {
			return nil, fmt.Errorf("todo.write: invalid input: %w", err)
		}
		a.todos = req.Todos
		resp, _ := json.Marshal(tools.TodoWriteResponse{Count: len(req.Todos)})
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

// handleTaskTool handles task.spawn/task.wait/task.cancel in-process via SubtaskRunner.
func (a *Agent) handleTaskTool(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	switch name {
	case "task_spawn":
		var req struct {
			Goal      string `json:"goal"`
			MaxSteps  int    `json:"max_steps"`
			TimeoutMS int    `json:"timeout_ms"`
		}
		if err := json.Unmarshal(input, &req); err != nil {
			return nil, fmt.Errorf("task.spawn: invalid input: %w", err)
		}
		if strings.TrimSpace(req.Goal) == "" {
			return nil, fmt.Errorf("task.spawn: goal is required")
		}
		taskID, err := a.opts.SubtaskRunner.Spawn(ctx, SubtaskSpawnRequest{
			Goal:      req.Goal,
			MaxSteps:  req.MaxSteps,
			TimeoutMS: req.TimeoutMS,
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
		return promptpkg.PlanModeReminder
	case ModeBuild, "":
		if a.justSwitchedFromPlan {
			a.justSwitchedFromPlan = false
			return promptpkg.BuildSwitchReminder
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
				results[idx] = string(out)
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
				// Successful call. Track for cross-step duplicate detection.
				// IsDuplicateCall is intentionally NOT consulted here: the
				// LLM emitted N parallel calls in one step, so dedupe of
				// within-batch repeats is the model's responsibility — but
				// recording each success means the NEXT step will detect a
				// repeat of these args.
				_ = cb.RecordSuccessfulCall(call.Name, call.Input)
				// N3 (audit ledger, Sprint 6): clear stale denial counter for
				// this tool — successful call means the block is gone.
				cb.ResetDeniedForTool(call.Name)
			}
		}
		if !anyErr && len(calls) > 0 {
			cb.ResetToolErrors()
		}
	}
	return history, nil
}
