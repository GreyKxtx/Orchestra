package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/internal/plan"
	"github.com/orchestra/orchestra/internal/tools"
)

// isAgentInProcessTool reports tools handled in the agent serial pipeline
// (session state, subtasks, skills, plan mode) rather than tools.Runner.Call.
func isAgentInProcessTool(name string) bool {
	switch normalizeToolName(name) {
	case "todowrite", "todoread",
		"task", "task_spawn", "task_wait", "task_cancel", "task_result",
		"plan_enter", "plan_exit", "question", "skill_invoke":
		return true
	default:
		return false
	}
}

// resolveToolCalls returns the tool calls for this step, preferring Step.Tools
// (multi-call) over Step.Tool (single). Names are normalized to canonical form.
func (a *Agent) resolveToolCalls(step *Step, llmResp *llm.CompleteResponse) []ToolCall {
	if step == nil {
		return nil
	}
	if len(step.Tools) > 0 {
		out := make([]ToolCall, len(step.Tools))
		for i, tc := range step.Tools {
			out[i] = ToolCall{
				ID:    tc.ID,
				Name:  normalizeToolName(tc.Name),
				Input: tc.Input,
			}
		}
		return out
	}
	if step.Tool != nil {
		tc := *step.Tool
		tc.Name = normalizeToolName(tc.Name)
		if tc.ID == "" && llmResp != nil && len(llmResp.Message.ToolCalls) > 0 {
			tc.ID = llmResp.Message.ToolCalls[0].ID
		}
		return []ToolCall{tc}
	}
	return nil
}

// allParallelSafeCalls reports whether every call may run via runParallelToolBatch.
// In-process agent tools are never parallel-safe even when the registry marks
// them ParallelSafe (e.g. todoread).
func allParallelSafeCalls(calls []ToolCall, defs []llm.ToolDef) bool {
	if len(calls) < 2 || len(defs) == 0 {
		return false
	}
	flag := make(map[string]bool, len(defs))
	for _, d := range defs {
		flag[d.Function.Name] = d.ParallelSafe
	}
	for _, c := range calls {
		name := normalizeToolName(c.Name)
		if isAgentInProcessTool(name) || !flag[name] {
			return false
		}
	}
	return true
}

// serialToolOutcome carries an early agent termination from a serial tool call.
type serialToolOutcome struct {
	EarlyResult *Result
	Err         error
}

// runSerialToolCall executes one tool call through the full serial pipeline
// (permissions, in-process handlers, Runner.Call). Appends tool messages to
// history. Returns a non-nil error for circuit-breaker trips; EarlyResult when
// the run should end (task_result child, plan_exit approved).
func (a *Agent) runSerialToolCall(ctx context.Context, cb *CircuitBreaker, history *[]llm.Message, tc ToolCall, steps int, emitStepDone func(string)) (serialToolOutcome, error) {
	name := normalizeToolName(tc.Name)
	if name == "" {
		return serialToolOutcome{}, nil
	}
	if name == "write" || name == "edit" {
		a.turnMutatingTools++
	}

	toolCallID := strings.TrimSpace(tc.ID)
	if toolCallID == "" {
		toolCallID = fmt.Sprintf("call_%d_%d", steps, time.Now().UnixNano())
	}

	effectiveAllowExec := a.opts.AllowExec
	effectiveAllowWeb := a.opts.AllowWeb
	if len(a.opts.PermissionRules) > 0 {
		subject := subjectForTool(name, tc.Input)
		if act, matched := checkPermissions(a.opts.PermissionRules, name, subject); matched {
			if act == "deny" {
				toolResult := formatToolDeniedJSON(name, tc.Input, "tool call denied by permission ruleset")
				*history = append(*history, llm.Message{
					Role:       llm.RoleTool,
					ToolCallID: toolCallID,
					Content:    toolResult,
				})
				if cbErr := cb.RecordDenied(name); cbErr != nil {
					return serialToolOutcome{}, cbErr
				}
				return serialToolOutcome{}, nil
			}
			effectiveAllowExec = true
			effectiveAllowWeb = true
		}
	}

	if name == "bash" && !effectiveAllowExec && a.opts.PermissionRequester != nil {
		cmdPreview := ""
		if len(tc.Input) > 0 {
			cmdPreview = string(tc.Input)
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
			toolResult := formatToolDeniedJSON(name, tc.Input, reason)
			*history = append(*history, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: toolCallID,
				Content:    toolResult,
			})
			if cbErr := cb.RecordDenied(name); cbErr != nil {
				return serialToolOutcome{}, cbErr
			}
			return serialToolOutcome{}, nil
		}
	}

	if name == "bash" && !effectiveAllowExec {
		cmd := execCommandFromInput(tc.Input)
		if !execCommandAllowed(cmd, a.opts.ExecAllow, a.opts.ExecDeny) {
			msg := "exec.run requires user consent (use --allow-exec or configure exec.allow)"
			if len(a.opts.ExecAllow) > 0 {
				msg = fmt.Sprintf("exec.run: command %q is not in the allowlist", cmd)
			}
			toolResult := formatToolDeniedJSON(name, tc.Input, msg)
			*history = append(*history, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: toolCallID,
				Content:    toolResult,
			})
			if cbErr := cb.RecordDenied(name); cbErr != nil {
				return serialToolOutcome{}, cbErr
			}
			return serialToolOutcome{}, nil
		}
	}

	if name == "webfetch" && !effectiveAllowWeb {
		toolResult := formatToolDeniedJSON(name, tc.Input, "webfetch requires user consent (use --allow-web)")
		*history = append(*history, llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: toolCallID,
			Content:    toolResult,
		})
		if cbErr := cb.RecordDenied(name); cbErr != nil {
			return serialToolOutcome{}, cbErr
		}
		return serialToolOutcome{}, nil
	}

	if name == "task_result" {
		if !a.opts.IsChild {
			*history = append(*history, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: toolCallID,
				Content:    formatToolErrorJSON(name, tc.Input, fmt.Errorf("task_result is only valid in subtask / skill_invoke child agents; main agents must emit a normal final response with patches")),
			})
			if cbErr := cb.RecordToolError(name); cbErr != nil {
				return serialToolOutcome{}, cbErr
			}
			return serialToolOutcome{}, nil
		}
		var req struct {
			Content string `json:"content"`
		}
		_ = json.Unmarshal(tc.Input, &req)
		emitStepDone("final")
		return serialToolOutcome{
			EarlyResult: &Result{
				Steps:         steps,
				SubtaskResult: req.Content,
				Todos:         a.todos,
			},
		}, nil
	}

	if a.opts.SkillRunner != nil && name == "skill_invoke" {
		out, skillErr := a.handleSkillInvoke(ctx, tc.Input)
		a.observeWorkingTool(name, tc.Input, out, skillErr)
		var content string
		if skillErr != nil {
			content = formatToolErrorJSON(name, tc.Input, skillErr)
		} else {
			content = string(out)
		}
		*history = append(*history, llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: toolCallID,
			Content:    content,
		})
		if skillErr != nil {
			if cbErr := cb.RecordToolError(name); cbErr != nil {
				return serialToolOutcome{}, cbErr
			}
		} else {
			cb.ResetToolErrors()
		}
		return serialToolOutcome{}, nil
	}

	if a.opts.SubtaskRunner != nil && (name == "task" || name == "task_spawn" || name == "task_wait" || name == "task_cancel") {
		out, taskErr := a.handleTaskTool(ctx, name, toolCallID, tc.Input)
		a.observeWorkingTool(name, tc.Input, out, taskErr)
		var content string
		if taskErr != nil {
			content = formatToolErrorJSON(name, tc.Input, taskErr)
		} else {
			content = string(out)
		}
		*history = append(*history, llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: toolCallID,
			Content:    content,
		})
		if taskErr != nil {
			if cbErr := cb.RecordToolError(name); cbErr != nil {
				return serialToolOutcome{}, cbErr
			}
		} else {
			cb.ResetToolErrors()
		}
		return serialToolOutcome{}, nil
	}

	if name == "todowrite" || name == "todoread" {
		out, err := a.handleTodoTool(name, tc.Input)
		a.observeWorkingTool(name, tc.Input, out, err)
		var content string
		if err != nil {
			content = formatToolErrorJSON(name, tc.Input, err)
		} else {
			content = string(out)
			if name == "todowrite" && a.opts.OnEvent != nil {
				payload, _ := json.Marshal(a.todos)
				a.opts.OnEvent(AgentEvent{Step: steps, Stream: llm.StreamEvent{
					Kind:    llm.StreamEventTodosUpdated,
					Content: string(payload),
				}})
			}
		}
		*history = append(*history, llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: toolCallID,
			Content:    content,
		})
		if err != nil {
			if cbErr := cb.RecordToolError(name); cbErr != nil {
				return serialToolOutcome{}, cbErr
			}
		} else {
			cb.ResetToolErrors()
		}
		return serialToolOutcome{}, nil
	}

	if name == "question" {
		var req struct {
			Questions []tools.QuestionItem `json:"questions"`
		}
		if qErr := json.Unmarshal(tc.Input, &req); qErr != nil || a.opts.QuestionAsker == nil {
			msg := `{"error":"question tool unavailable"}`
			if a.opts.QuestionAsker != nil {
				msg = formatToolErrorJSON(name, tc.Input, qErr)
			}
			*history = append(*history, llm.Message{Role: llm.RoleTool, ToolCallID: toolCallID, Content: msg})
			if cbErr := cb.RecordToolError(name); cbErr != nil {
				return serialToolOutcome{}, cbErr
			}
			return serialToolOutcome{}, nil
		}
		answers, qErr := a.opts.QuestionAsker.Ask(ctx, req.Questions)
		var content string
		if qErr != nil {
			content = formatToolErrorJSON(name, tc.Input, qErr)
			if cbErr := cb.RecordToolError(name); cbErr != nil {
				return serialToolOutcome{}, cbErr
			}
		} else {
			b, _ := json.Marshal(map[string]any{"answers": answers})
			content = string(b)
			cb.ResetToolErrors()
		}
		*history = append(*history, llm.Message{Role: llm.RoleTool, ToolCallID: toolCallID, Content: content})
		return serialToolOutcome{}, nil
	}

	if name == "plan_exit" {
		approved := false
		if a.opts.QuestionAsker != nil {
			answers, qErr := a.opts.QuestionAsker.Ask(ctx, []tools.QuestionItem{{
				Question: "Plan complete. Switch to build mode to apply changes?",
				Options:  []string{"Yes, switch to build", "No, keep planning"},
			}})
			if qErr == nil && len(answers) > 0 {
				ans := strings.ToLower(strings.TrimSpace(answers[0]))
				approved = ans == "1" || ans == "yes" || ans == "y" || strings.HasPrefix(ans, "yes,") || ans == "да" || strings.HasPrefix(ans, "да,")
			}
		} else {
			*history = append(*history, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: toolCallID,
				Content:    `{"status":"refused","message":"plan_exit is unavailable in non-interactive mode. Finish with a final answer — the user will switch modes manually if needed."}`,
			})
			return serialToolOutcome{}, nil
		}
		if approved {
			emitStepDone("final")
			return serialToolOutcome{
				EarlyResult: &Result{Steps: steps, SwitchToBuild: true, Todos: a.todos},
			}, nil
		}
		*history = append(*history, llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: toolCallID,
			Content:    `{"status":"continue","message":"Continue planning. Refine the plan and call plan_exit again when ready."}`,
		})
		return serialToolOutcome{}, nil
	}

	if name == "plan_enter" {
		*history = append(*history, llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: toolCallID,
			Content:    `{"status":"not_supported","message":"plan_enter недоступен в текущем режиме. Запусти orchestra apply --mode plan для планирования."}`,
		})
		return serialToolOutcome{}, nil
	}

	if (a.opts.Mode == ModePlan || a.opts.Mode == ModeOrchestra || a.opts.Mode == ModeArchitecture) && (name == "write" || name == "edit") {
		var pathReq struct {
			Path string `json:"path"`
		}
		allowed := false
		if json.Unmarshal(tc.Input, &pathReq) == nil {
			allowed = plan.IsWritablePath(pathReq.Path, a.effectivePlanPath())
		}
		if !allowed {
			label := "plan mode"
			switch a.opts.Mode {
			case ModeOrchestra:
				label = "orchestra lead"
			case ModeArchitecture:
				label = "architecture mode"
			}
			toolResult := formatToolDeniedJSON(name, tc.Input, fmt.Sprintf("%s: writes are allowed only to %s or .orchestra/plans/*.md", label, a.effectivePlanPath()))
			*history = append(*history, llm.Message{Role: llm.RoleTool, ToolCallID: toolCallID, Content: toolResult})
			if cbErr := cb.RecordDenied(name); cbErr != nil {
				return serialToolOutcome{}, cbErr
			}
			return serialToolOutcome{}, nil
		}
	}
	if a.opts.Mode == ModeAsk && (name == "write" || name == "edit" || name == "bash" || name == "fs.delete" || name == "fs.rename" || name == "skill_invoke") {
		toolResult := formatToolDeniedJSON(name, tc.Input, "ask mode is read-only")
		*history = append(*history, llm.Message{Role: llm.RoleTool, ToolCallID: toolCallID, Content: toolResult})
		if cbErr := cb.RecordDenied(name); cbErr != nil {
			return serialToolOutcome{}, cbErr
		}
		return serialToolOutcome{}, nil
	}

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

	if a.opts.HooksRunner != nil {
		hookErr := safeRunErr("PreTool hook "+name, func() error {
			return a.opts.HooksRunner.RunPreTool(callCtx, name, tc.Input)
		})
		if hookErr != nil {
			toolResult := formatToolDeniedJSON(name, tc.Input, "pre-tool hook denied: "+hookErr.Error())
			*history = append(*history, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: toolCallID,
				Content:    toolResult,
			})
			if cbErr := cb.RecordDenied(name); cbErr != nil {
				return serialToolOutcome{}, cbErr
			}
			return serialToolOutcome{}, nil
		}
	}

	if a.opts.AgentLogger != nil {
		a.opts.AgentLogger.LogToolCall(name, len(tc.Input))
	}

	if dedupExemptTool(name) {
		if cb.IsReadOnlyBlocked(name, tc.Input) {
			stopMsg := "⛔ The tool «" + name + "» was called too many times with identical arguments — the result is already in your history. Proceed with edit/write or use different arguments."
			a.logf("tool_call name=%s read_only_doom_blocked", name)
			*history = append(*history, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: toolCallID,
				Content:    stopMsg,
			})
			return serialToolOutcome{}, nil
		}
	} else if cb.IsDuplicateCall(name, tc.Input) {
		stopMsg := "⛔ The tool «" + name + "» was already called with these exact arguments — duplicate blocked. Use edit/write to apply changes or call with different arguments."
		a.logf("tool_call name=%s dedup_blocked", name)
		if a.opts.OnEvent != nil {
			a.opts.OnEvent(AgentEvent{Step: steps, Stream: llm.StreamEvent{
				Kind:         llm.StreamEventToolCallCompleted,
				ToolCallName: name,
				ToolCallID:   toolCallID,
				Content:      "[dedup blocked]",
			}})
		}
		*history = append(*history, llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: toolCallID,
			Content:    stopMsg,
		})
		return serialToolOutcome{}, nil
	}

	start := time.Now()
	out, err := a.tools.Call(callCtx, name, tc.Input)
	dur := time.Since(start).Milliseconds()
	if err != nil {
		a.observeWorkingTool(name, tc.Input, out, err)
	}

	if a.opts.OnEvent != nil {
		a.opts.OnEvent(AgentEvent{Step: steps, Stream: toolCallCompletedStreamEvent(name, toolCallID, out, err)})
	}

	if err != nil {
		a.logf("tool_call name=%s status=error duration_ms=%d err=%v", name, dur, err)
		if a.opts.AgentLogger != nil {
			a.opts.AgentLogger.LogToolResult(name, 0, dur, err.Error())
		}
		toolResult := formatToolErrorJSON(name, tc.Input, err)
		*history = append(*history, llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: toolCallID,
			Content:    toolResult,
		})
		if cbErr := cb.RecordToolError(name); cbErr != nil {
			return serialToolOutcome{}, cbErr
		}
		return serialToolOutcome{}, nil
	}

	if a.opts.HooksRunner != nil {
		_ = safeRunErr("PostTool hook "+name, func() error {
			a.opts.HooksRunner.RunPostTool(callCtx, name, out)
			return nil
		})
	}
	a.logf("tool_call name=%s status=ok duration_ms=%d output_bytes=%d", name, dur, len(out))
	if a.opts.AgentLogger != nil {
		a.opts.AgentLogger.LogToolResult(name, len(out), dur, "")
	}
	*history = append(*history, llm.Message{
		Role:       llm.RoleTool,
		ToolCallID: toolCallID,
		Content:    a.prepareToolHistoryContent(name, tc.Input, out),
	})

	if name == "write" || name == "edit" {
		a.commitStagedAfterMutatingTool(ctx, steps, extractWriteOrEditPath(tc.Input))
	}

	if a.opts.MultimodalLLM && name == "browser.screenshot" {
		if part, ok := extractScreenshotImagePart(out); ok {
			*history = append(*history, llm.Message{
				Role: llm.RoleUser,
				Parts: []llm.ContentPart{
					{Kind: llm.PartText, Text: "Screenshot returned by browser.screenshot:"},
					part,
				},
			})
		}
	}

	if name == "write" || name == "edit" {
		if hint := extractLSPErrors(out); hint != "" {
			path := extractWriteOrEditPath(tc.Input)
			streak := a.diags.Observe(path, fingerprintLSPErrors(out))
			if streak >= 2 && path != "" {
				hint = "LSP_ERRORS — your last edit on " + path + " did not change diagnostics (same error set, attempt #" + fmt.Sprint(streak) + "). Stop write/edit'ing this file and diagnose the cause via lsp.references / lsp.hover / read.\n" + hint
			}
			a.logf("lsp_hint name=%s path=%s streak=%d injecting diagnostic hint", name, path, streak)
			*history = append(*history, llm.Message{
				Role:    llm.RoleUser,
				Content: hint,
			})
			if a.opts.OnEvent != nil {
				a.opts.OnEvent(AgentEvent{Step: steps, Stream: llm.StreamEvent{
					Kind:    llm.StreamEventRecoverableError,
					Content: "lsp_errors: " + name,
				}})
			}
		} else {
			_ = a.diags.Observe(extractWriteOrEditPath(tc.Input), "")
		}
	}

	if dedupExemptTool(name) {
		if hint := cb.RecordReadOnlyCall(name, tc.Input); hint != "" {
			*history = append(*history, llm.Message{Role: llm.RoleUser, Content: hint})
		}
	} else if dupHint := cb.RecordSuccessfulCall(name, tc.Input); dupHint != "" {
		*history = append(*history, llm.Message{Role: llm.RoleUser, Content: dupHint})
	}
	a.logf("agent.tool_call added tool message to history, history_len=%d, tool_call_id=%s", len(*history), toolCallID)
	cb.ResetToolErrors()
	cb.ResetDeniedForTool(name)
	cb.ResetFinalFailures()
	return serialToolOutcome{}, nil
}
