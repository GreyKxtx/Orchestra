package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
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
	// The callback must not block; use a goroutine or buffered channel if you need async processing.
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

	// HooksRunner, if non-nil, runs pre/post tool call hooks.
	HooksRunner HooksRunner

	// CompactThresholdPct, if > 0, triggers history compaction when total history size (in bytes)
	// exceeds this percentage of MaxPromptBytes. 0 = disabled. Recommended: 70.
	// Compaction failure is non-fatal: logs a warning and continues without compacting.
	CompactThresholdPct int

	Debug  bool
	Logger *log.Logger
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

	for steps < a.opts.MaxSteps {
		steps++

		// Compaction: if history is getting large, summarise it before the next LLM call.
		// Fires only at the top of the loop so history is always in a consistent state
		// (no orphaned tool_calls without tool_results).
		if a.opts.CompactThresholdPct > 0 && a.opts.MaxPromptBytes > 0 {
			threshold := a.opts.MaxPromptBytes * a.opts.CompactThresholdPct / 100
			if historyBytes(history) > threshold {
				compacted, compactErr := a.compactHistory(ctx, userQuery, history)
				if compactErr != nil {
					a.logf("compaction failed (non-fatal), continuing with truncation: %v", compactErr)
				} else {
					a.logf("history compacted: %d bytes → %d bytes", historyBytes(history), historyBytes(compacted))
					history = compacted
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
			if step.Tool == nil {
				// Add validation error as user message for retry
				history = append(history, llm.Message{
					Role:    llm.RoleUser,
					Content: formatValidatorError("Invalid JSON format: tool is required", raw),
				})
				continue
			}
			name := strings.TrimSpace(step.Tool.Name)
			if name == "" {
				history = append(history, llm.Message{
					Role:    llm.RoleUser,
					Content: formatValidatorError("Invalid JSON format: tool.name is empty", raw),
				})
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
						continue
					}
					// act == "allow": grant consent for this call only.
					effectiveAllowExec = true
					effectiveAllowWeb = true
				}
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
				continue
			}

			// task.result: child agent reports its answer and exits immediately.
			if name == "task_result" {
				var req struct {
					Content string `json:"content"`
				}
				_ = json.Unmarshal(step.Tool.Input, &req)
				return history, &Result{
					Steps:         steps,
					SubtaskResult: req.Content,
					Todos:         a.todos,
				}, nil
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
					approved = true // non-interactive (CI): auto-approve
				}
				if approved {
					return history, &Result{Steps: steps, SwitchToBuild: true, Todos: a.todos}, nil
				}
				history = append(history, llm.Message{
					Role:       llm.RoleTool,
					ToolCallID: toolCallID,
					Content:    `{"status":"continue","message":"Продолжаем планирование. Доработай план и вызови plan_exit снова."}`,
				})
				continue
			}

			// plan_enter: stub — switching modes in-process is not supported yet.
			if name == "plan_enter" {
				history = append(history, llm.Message{
					Role:       llm.RoleTool,
					ToolCallID: toolCallID,
					Content:    `{"status":"not_supported","message":"plan_enter недоступен в текущем режиме. Запусти orchestra apply --mode plan для планирования."}`,
				})
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

			// Pre-tool hook: non-zero exit denies the tool call.
			if a.opts.HooksRunner != nil {
				if hookErr := a.opts.HooksRunner.RunPreTool(callCtx, name, step.Tool.Input); hookErr != nil {
					toolResult := formatToolDeniedJSON(name, step.Tool.Input, "pre-tool hook denied: "+hookErr.Error())
					history = append(history, llm.Message{
						Role:       llm.RoleTool,
						ToolCallID: toolCallID,
						Content:    toolResult,
					})
					if cbErr := cb.RecordDenied(name); cbErr != nil {
						return nil, nil, cbErr
					}
					continue
				}
			}

			a.opts.AgentLogger.LogToolCall(name, len(step.Tool.Input))
			start := time.Now()
			out, err := a.tools.Call(callCtx, name, step.Tool.Input)
			dur := time.Since(start).Milliseconds()
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
				continue
			}
			// Post-tool hook: errors logged but do not fail the tool.
			if a.opts.HooksRunner != nil {
				a.opts.HooksRunner.RunPostTool(callCtx, name, out)
			}
			a.logf("tool_call name=%s status=ok duration_ms=%d output_bytes=%d", name, dur, len(out))
			a.opts.AgentLogger.LogToolResult(name, len(out), dur, "")
			history = append(history, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: toolCallID,
				Content:    string(out),
			})
			a.logf("agent.tool_call added tool message to history, history_len=%d, tool_call_id=%s", len(history), toolCallID)
			cb.ResetToolErrors()
			cb.ResetFinalFailures() // successful tool call = model is making progress
			continue

		case StepFinal:
			if step.Final == nil {
				history = append(history, llm.Message{
					Role:    llm.RoleUser,
					Content: formatValidatorError("Invalid JSON format: final is required", raw),
				})
				continue
			}
			// Empty patches is valid (no changes needed)
			if len(step.Final.Patches) == 0 {
				a.logf("final received empty patches (no changes needed)")
				if llmResp != nil {
					history = append(history, llmResp.Message)
				}
				return history, &Result{
					Patches: []patches.Patch{},
					Applied: false,
					Todos:   a.todos,
				}, nil
			}

			patches := append([]patches.Patch(nil), step.Final.Patches...)
			a.logf("final received patches=%d", len(patches))

			start := time.Now()
			internalOps, err := resolver.ResolveExternalPatches(a.tools.WorkspaceRoot(), patches)
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
				continue
			}
			a.logf("resolve status=ok duration_ms=%d ops=%d", resolveMS, len(internalOps))

			applyReq := tools.FSApplyOpsRequest{
				Ops:    internalOps,
				DryRun: !a.opts.Apply,
				Backup: a.opts.Backup && a.opts.Apply,
			}

			start = time.Now()
			resp, err := a.tools.FSApplyOps(ctx, applyReq)
			applyMS := time.Since(start).Milliseconds()
			if err != nil {
				// StaleContent/AmbiguousMatch are recoverable: keep looping.
				if pe, ok := protocol.AsError(err); ok && (pe.Code == protocol.StaleContent || pe.Code == protocol.AmbiguousMatch) {
					a.logf("apply status=recoverable_error duration_ms=%d err=%v", applyMS, err)
					history = append(history, llm.Message{
						Role:    llm.RoleUser,
						Content: formatApplyErrorCompact(err, pe.Code),
					})
					if cbErr := cb.RecordFinalFailure(err); cbErr != nil {
						return nil, nil, cbErr
					}
					continue
				}
				a.logf("apply status=error duration_ms=%d err=%v", applyMS, err)
				return nil, nil, err
			}
			a.logf("apply status=ok duration_ms=%d diffs=%d dry_run=%v", applyMS, len(resp.Diffs), applyReq.DryRun)

			if llmResp != nil {
				history = append(history, llmResp.Message)
			}
			return history, &Result{
				Steps:         steps,
				Patches:       patches,
				Ops:           internalOps,
				Applied:       a.opts.Apply,
				ApplyResponse: resp,
				Todos:         a.todos,
			}, nil

		default:
			history = append(history, llm.Message{
				Role:    llm.RoleUser,
				Content: formatValidatorError("Invalid JSON format: unknown step type", raw),
			})
		}
	}

	return nil, nil, protocol.NewError(protocol.InvalidLLMOutput, "max_steps exceeded", map[string]any{
		"max_steps": a.opts.MaxSteps,
	})
}

// buildToolDefs returns the tool definitions for this agent run.
// Uses CustomTools when set (child agents), otherwise picks based on Mode,
// and appends ExtraTools (e.g. MCP server tools).
func (a *Agent) buildToolDefs() []llm.ToolDef {
	if len(a.opts.CustomTools) > 0 {
		return a.opts.CustomTools
	}
	allowExec := a.opts.AllowExec || len(a.opts.ExecAllow) > 0
	allowWeb := a.opts.AllowWeb
	hasSubtasks := a.opts.SubtaskRunner != nil
	hasQA := a.opts.QuestionAsker != nil

	var base []llm.ToolDef
	if a.opts.Mode != "" {
		base = tools.ListToolsForMode(a.opts.Mode, allowExec, allowWeb, hasSubtasks, hasQA)
	} else if hasSubtasks {
		base = tools.ListToolsWithSubtasks(allowExec, allowWeb)
	} else {
		base = tools.ListTools(allowExec, allowWeb)
	}
	if len(a.opts.ExtraTools) > 0 {
		base = append(base, a.opts.ExtraTools...)
	}
	return base
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
	messages = append(messages, llm.Message{
		Role:    llm.RoleUser,
		Content: userPrompt,
	})
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
			totalBytes += len(m.Content)
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
		step, raw, nerr := NormalizeLLM(a.validator, resp)
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

// formatResolveErrorCompact returns a compact resolve error message.
func formatResolveErrorCompact(err error) string {
	if pe, ok := protocol.AsError(err); ok {
		return fmt.Sprintf("RESOLVE_ERROR code=%s\nПеречитай файл (fs.read) и обнови file_hash в патче.", pe.Code)
	}
	return "RESOLVE_ERROR code=unknown\nerror=" + err.Error() + "\nПеречитай файл (fs.read) и обнови file_hash в патче."
}

func formatApplyError(err error) string {
	return "APPLY_ERROR\nerror=" + formatErr(err)
}

// formatApplyErrorCompact returns a compact apply error message with actionable hint.
func formatApplyErrorCompact(err error, code protocol.ErrorCode) string {
	if code == protocol.StaleContent {
		return "APPLY_ERROR code=StaleContent\nФайл изменился. Перечитай файл (fs.read) и обнови патч с новым file_hash."
	}
	if code == protocol.AmbiguousMatch {
		return "APPLY_ERROR code=AmbiguousMatch\nПоиск неоднозначен. Уточни search-блок в патче (добавь больше контекста)."
	}
	return "APPLY_ERROR code=unknown\nerror=" + formatErr(err)
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

// truncateMessages truncates message history to fit within byte budget.
// Always keeps system and first user message, then keeps as many history messages as possible.
// Optimized to preserve assistant+tool pairs and account for tool_calls/ToolCallID overhead.
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
		// Worst case: return only required messages
		return required
	}

	budget := maxBytes - requiredSize
	var selected []llm.Message
	size := 0
	used := make(map[int]bool, len(messages)) // Track which messages we've already added

	// Keep history messages from the end (most recent first)
	// Try to preserve assistant+tool pairs together
	// Note: In history, order is: assistant (with tool_calls) -> tool (with result)
	for i := len(messages) - 1; i >= 2; i-- {
		if used[i] {
			continue // Already added as part of a pair
		}

		msg := messages[i]
		msgSize := estimateMessageSize(msg)

		// If this is a tool message, try to include its corresponding assistant message
		// In history, assistant comes BEFORE tool, so we check i-1
		if msg.Role == llm.RoleTool && i > 2 && !used[i-1] {
			// Check if previous message is assistant with matching tool_call
			prevMsg := messages[i-1]
			if prevMsg.Role == llm.RoleAssistant && len(prevMsg.ToolCalls) > 0 {
				// Check if tool_call_id matches
				if msg.ToolCallID != "" {
					for _, tc := range prevMsg.ToolCalls {
						if tc.ID == msg.ToolCallID {
							// Include both as a pair
							// We're iterating backwards, so we add: tool, then assistant
							// After reverse: assistant, then tool (correct order)
							pairSize := estimateMessageSize(prevMsg) + msgSize
							if size+pairSize <= budget {
								selected = append(selected, msg)     // tool first (will be last after reverse)
								selected = append(selected, prevMsg) // assistant second (will be first after reverse)
								size += pairSize
								used[i] = true
								used[i-1] = true
								i-- // Skip assistant message in next iteration
								continue
							}
						}
					}
				}
			}
		}

		// Single message (or pair didn't fit)
		if size+msgSize > budget {
			break
		}
		selected = append(selected, msg)
		size += msgSize
		used[i] = true
	}

	// Reverse to restore chronological order
	// After reverse: assistant messages come before their corresponding tool messages
	for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
		selected[i], selected[j] = selected[j], selected[i]
	}

	result := make([]llm.Message, 0, len(required)+len(selected))
	result = append(result, required...)
	result = append(result, selected...)
	return result
}

// estimateMessageSize estimates the byte size of a message for truncation purposes.
// Accounts for Content, ToolCalls (JSON serialization overhead), and ToolCallID.
func estimateMessageSize(msg llm.Message) int {
	size := len(msg.Content)
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
	return size
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
