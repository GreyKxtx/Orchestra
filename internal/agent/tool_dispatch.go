package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/internal/toolspec"
	"github.com/orchestra/orchestra/llm"
)

// isAgentInProcessTool reports tools handled in the agent serial pipeline
// (session state, subtasks, skills, agency, plan mode) rather than
// tools.Runner.Call.
func isAgentInProcessTool(name string) bool {
	return toolspec.IsInProcess(normalizeToolName(name))
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

// isWebTool is a tool that reaches the network on the model's behalf and needs
// web consent: webfetch sends the URL, websearch sends the query.
func isWebTool(name string) bool {
	return toolspec.IsWeb(name)
}

// batchNeedsSerialGates reports whether a call in the batch has a gate that
// only the serial path applies: a permission rule matching it, or a web or
// browser tool this run has no consent for. The parallel batch checks none of
// these, so two reads a deny rule covers, or two web calls in a mode that lists
// them without consent (product), ran unchecked. Such a batch runs serially.
func (a *Agent) batchNeedsSerialGates(calls []ToolCall) bool {
	for _, c := range calls {
		name := normalizeToolName(c.Name)
		if len(a.opts.PermissionRules) > 0 {
			if _, matched := checkPermissions(a.opts.PermissionRules, name, subjectForTool(name, c.Input)); matched {
				return true
			}
		}
		if (isWebTool(name) && !a.opts.AllowWeb) || a.browserCallRefusal(name) != nil || a.mcpCallNeedsConsent(name) {
			return true
		}
	}
	return false
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

// recordBlocked records a refusal that happens AFTER the call has been logged,
// and whose message to the model is prose rather than the denied-JSON shape.
//
// The read-only and duplicate blockers are refusals by every measure that
// matters — they stop the call and they spend the denied-repeat budget — but
// they left a tool_call in the trace with no result beside it. A turn that
// died on a repeated write therefore read as a turn where six writes simply
// vanished, which is how this was nearly diagnosed as a guard misfiring.
func (a *Agent) recordBlocked(name, reason string) {
	if a.opts.AgentLogger != nil {
		// The reason is already the error field; a preview would repeat it.
		a.opts.AgentLogger.LogToolResult(name, 0, 0, "denied: "+reason, "")
	}
}

// deniedToolResult formats a refusal and records that it happened.
//
// Refusals return before LogToolCall, so a denied call used to leave no trace
// at all: a turn killed by repeated refusals showed, in llm_log.jsonl, only
// the tools that got through. An orchestra Lead that spent its whole turn
// being refused for `write` appeared never to have called write — the trace
// showed the delegation it also attempted and nothing else, which is exactly
// backwards from what a reader needs to see.
//
// Logged as a call plus a result carrying the reason, so the count of
// attempts and what was said each time both survive the run.
func (a *Agent) deniedToolResult(name string, input json.RawMessage, reason string) string {
	if a.opts.AgentLogger != nil {
		a.opts.AgentLogger.LogToolCall(name, len(input), string(input))
		a.opts.AgentLogger.LogToolResult(name, 0, 0, "denied: "+reason, "")
	}
	a.logf("tool_call name=%s status=denied reason=%s", name, reason)
	return formatToolDeniedJSON(name, input, reason)
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
	toolCallID := strings.TrimSpace(tc.ID)
	if toolCallID == "" {
		toolCallID = fmt.Sprintf("call_%d_%d", steps, time.Now().UnixNano())
	}
	refusal := a.browserCallRefusal(name)
	if refusal == nil {
		refusal = a.mcpModeRefusal(name)
	}
	if refusal == nil {
		refusal = a.offeredToolRefusal(name)
	}
	if refusal != nil {
		toolResult := a.deniedToolResult(name, tc.Input, refusal.Error())
		*history = append(*history, llm.Message{Role: llm.RoleTool, ToolCallID: toolCallID, Content: toolResult})
		if cbErr := cb.RecordDenied(name); cbErr != nil {
			return serialToolOutcome{}, cbErr
		}
		return serialToolOutcome{}, nil
	}
	if scopeErr := a.checkExploreFirstGate(name, *history); scopeErr != nil {
		toolResult := a.deniedToolResult(name, tc.Input, scopeErr.Error())
		*history = append(*history, llm.Message{Role: llm.RoleTool, ToolCallID: toolCallID, Content: toolResult})
		if cbErr := cb.RecordDenied(name); cbErr != nil {
			return serialToolOutcome{}, cbErr
		}
		return serialToolOutcome{}, nil
	}

	effectiveAllowExec := a.opts.AllowExec
	effectiveAllowWeb := a.opts.AllowWeb
	// A rule that allows the call, or asks and gets a yes, is the MCP consent.
	mcpConsented := false
	if len(a.opts.PermissionRules) > 0 {
		subject := subjectForTool(name, tc.Input)
		if act, matched := checkPermissions(a.opts.PermissionRules, name, subject); matched {
			switch act {
			case "deny":
				toolResult := a.deniedToolResult(name, tc.Input, "tool call denied by permission ruleset")
				*history = append(*history, llm.Message{
					Role:       llm.RoleTool,
					ToolCallID: toolCallID,
					Content:    toolResult,
				})
				if cbErr := cb.RecordDenied(name); cbErr != nil {
					return serialToolOutcome{}, cbErr
				}
				return serialToolOutcome{}, nil
			case "allow":
				effectiveAllowExec = true
				effectiveAllowWeb = true
				mcpConsented = true
			case "ask":
				approved, permErr := a.requestInteractivePermission(ctx, name, subject, tc.Input)
				if permErr != nil || !approved {
					reason := "tool call denied by interactive permission requester"
					if permErr != nil {
						reason = permErr.Error()
					}
					toolResult := a.deniedToolResult(name, tc.Input, reason)
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
				mcpConsented = true
			}
		}
	}

	if !mcpConsented && a.mcpCallNeedsConsent(name) {
		if approved, reason := a.requestMCPConsent(ctx, name, string(tc.Input)); !approved {
			toolResult := a.deniedToolResult(name, tc.Input, reason)
			*history = append(*history, llm.Message{Role: llm.RoleTool, ToolCallID: toolCallID, Content: toolResult})
			if cbErr := cb.RecordDenied(name); cbErr != nil {
				return serialToolOutcome{}, cbErr
			}
			return serialToolOutcome{}, nil
		}
	}

	if name == "bash" && !effectiveAllowExec && a.opts.PermissionRequester != nil {
		cmd, args := execCommandFromInput(tc.Input)
		if len(args) > 0 {
			cmd += " " + strings.Join(args, " ")
		}
		if strings.TrimSpace(cmd) == "" {
			cmd = string(tc.Input)
		}
		resp, permErr := a.opts.PermissionRequester.RequestPermission(ctx, PermissionRequest{
			Tool:        "bash",
			Description: permissionText(cmd),
		})
		if permErr == nil && resp.Approved {
			effectiveAllowExec = true
		} else if permErr == nil && !resp.Approved {
			reason := "exec.run denied by interactive permission requester"
			if resp.Reason != "" {
				reason = resp.Reason
			}
			toolResult := a.deniedToolResult(name, tc.Input, reason)
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
		cmd, args := execCommandFromInput(tc.Input)
		if ok, why := execCommandAllowed(cmd, args, a.opts.ExecAllow, a.opts.ExecDeny); !ok {
			msg := "exec.run requires user consent (use --allow-exec or configure exec.allow)"
			if len(a.opts.ExecAllow) > 0 {
				msg = fmt.Sprintf("exec.run: command %q is not covered by the allowlist: %s", cmd, why)
			}
			toolResult := a.deniedToolResult(name, tc.Input, msg)
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

	if isWebTool(name) && !effectiveAllowWeb {
		toolResult := a.deniedToolResult(name, tc.Input, name+" requires user consent (use --allow-web, or web.confirm: false)")
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
		// An empty content ends the child with nothing to say, and an empty
		// answer is read downstream as a successful one: workerTaskResultSuccess
		// treats an absent status as success, so the Lead is handed
		// {"status":"verified_success","worker_result":""} — told the job is
		// done and given no account of it. The common way to get here is a
		// model guessing the argument name ({"result":...}, {"summary":...}):
		// the unmarshal above ignores the mismatch and leaves Content blank.
		// Ask for the answer again instead of finishing without one.
		if strings.TrimSpace(req.Content) == "" {
			*history = append(*history, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: toolCallID,
				Content: formatToolErrorJSON(name, tc.Input, fmt.Errorf(
					"task_result needs its content argument: the result goes in \"content\" "+
						"as a string, and every other key is ignored. Resend task_result "+
						"with {\"content\": \"<your result>\"}")),
			})
			if cbErr := cb.RecordToolError(name); cbErr != nil {
				return serialToolOutcome{}, cbErr
			}
			return serialToolOutcome{}, nil
		}
		if schemaErr := a.checkWorkerResultSchema(req.Content); schemaErr != nil {
			*history = append(*history, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: toolCallID,
				Content:    formatToolErrorJSON(name, tc.Input, schemaErr),
			})
			if cbErr := cb.RecordToolError(name); cbErr != nil {
				return serialToolOutcome{}, cbErr
			}
			return serialToolOutcome{}, nil
		}
		if taxErr := checkBlockedReasonTaxonomy(req.Content); taxErr != nil {
			*history = append(*history, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: toolCallID,
				Content:    formatToolErrorJSON(name, tc.Input, taxErr),
			})
			if cbErr := cb.RecordToolError(name); cbErr != nil {
				return serialToolOutcome{}, cbErr
			}
			return serialToolOutcome{}, nil
		}
		if blockErr := a.blockWorkerTaskResult(req.Content); blockErr != nil {
			*history = append(*history, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: toolCallID,
				Content:    formatToolErrorJSON(name, tc.Input, blockErr),
			})
			if cbErr := cb.RecordToolError(name); cbErr != nil {
				return serialToolOutcome{}, cbErr
			}
			return serialToolOutcome{}, nil
		}
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
		// In-process tools bypass tools.Runner, so mirror its llm_log entries
		// here — otherwise a delegated subtask is indistinguishable, in the
		// log, from work the parent did inline. The eval's tool_used check
		// reads these entries, so without them `tool_used: skill_invoke` can
		// never pass however well the model delegates.
		if a.opts.AgentLogger != nil {
			a.opts.AgentLogger.LogToolCall(name, len(tc.Input), string(tc.Input))
		}
		skillStart := time.Now()
		out, skillErr := a.handleSkillInvoke(ctx, tc.Input)
		if a.opts.AgentLogger != nil {
			errStr := ""
			if skillErr != nil {
				errStr = skillErr.Error()
			}
			a.opts.AgentLogger.LogToolResult(name, len(out), time.Since(skillStart).Milliseconds(), errStr, string(out))
		}
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
			if cbErr := cb.RecordToolErrorDetail(name, skillErr); cbErr != nil {
				return serialToolOutcome{}, cbErr
			}
		} else {
			cb.ResetToolErrors()
		}
		return serialToolOutcome{}, nil
	}

	if a.opts.SubtaskRunner != nil && (name == "task" || name == "task_spawn" || name == "task_wait" || name == "task_cancel" || IsAgencyTool(name)) {
		// In-process tools bypass tools.Runner, so mirror its llm_log entries
		// here — otherwise failed spawns leave no trace in .orchestra logs.
		if a.opts.AgentLogger != nil {
			a.opts.AgentLogger.LogToolCall(name, len(tc.Input), string(tc.Input))
		}
		taskStart := time.Now()
		out, taskErr := a.handleTaskTool(ctx, name, toolCallID, tc.Input)
		if a.opts.AgentLogger != nil {
			errStr := ""
			if taskErr != nil {
				errStr = taskErr.Error()
			}
			a.opts.AgentLogger.LogToolResult(name, len(out), time.Since(taskStart).Milliseconds(), errStr, string(out))
		}
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
			if cbErr := cb.RecordToolErrorDetail(name, taskErr); cbErr != nil {
				return serialToolOutcome{}, cbErr
			}
		} else {
			cb.ResetToolErrors()
		}
		return serialToolOutcome{}, nil
	}

	if name == "todowrite" || name == "todoread" {
		// Mirrored for the same reason as skill_invoke above: the checklist is
		// otherwise invisible to anyone reading the log, and to the eval check
		// written to prove the model keeps one.
		if a.opts.AgentLogger != nil {
			a.opts.AgentLogger.LogToolCall(name, len(tc.Input), string(tc.Input))
		}
		todoStart := time.Now()
		out, err := a.handleTodoTool(name, tc.Input)
		if a.opts.AgentLogger != nil {
			errStr := ""
			if err != nil {
				errStr = err.Error()
			}
			a.opts.AgentLogger.LogToolResult(name, len(out), time.Since(todoStart).Milliseconds(), errStr, string(out))
		}
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

	if name == "contract_freeze" {
		out, err := a.handleContractFreeze(ctx)
		a.observeWorkingTool(name, tc.Input, out, err)
		var content string
		if err != nil {
			content = formatToolErrorJSON(name, tc.Input, err)
		} else {
			content = string(out)
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

	if name == "lesson_promote" {
		out, err := a.handleLessonPromote(ctx, tc.Input)
		a.observeWorkingTool(name, tc.Input, out, err)
		var content string
		if err != nil {
			content = formatToolErrorJSON(name, tc.Input, err)
		} else {
			content = string(out)
		}
		*history = append(*history, llm.Message{Role: llm.RoleTool, ToolCallID: toolCallID, Content: content})
		if err != nil {
			if cbErr := cb.RecordToolError(name); cbErr != nil {
				return serialToolOutcome{}, cbErr
			}
		} else {
			cb.ResetToolErrors()
		}
		return serialToolOutcome{}, nil
	}

	if name == "playbook_promote" {
		out, err := a.handlePlaybookPromote(ctx, tc.Input)
		a.observeWorkingTool(name, tc.Input, out, err)
		var content string
		if err != nil {
			content = formatToolErrorJSON(name, tc.Input, err)
		} else {
			content = string(out)
		}
		*history = append(*history, llm.Message{Role: llm.RoleTool, ToolCallID: toolCallID, Content: content})
		if err != nil {
			if cbErr := cb.RecordToolError(name); cbErr != nil {
				return serialToolOutcome{}, cbErr
			}
		} else {
			cb.ResetToolErrors()
		}
		return serialToolOutcome{}, nil
	}

	if name == "update_working_state" {
		out, err := a.handleUpdateWorkingState(tc.Input)
		a.observeWorkingTool(name, tc.Input, out, err)
		var content string
		if err != nil {
			content = formatToolErrorJSON(name, tc.Input, err)
		} else {
			content = string(out)
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
		qErr := json.Unmarshal(tc.Input, &req)
		// A model that flattens the argument — {"question": "..."} instead of
		// {"questions": [{"question": "..."}]} — unmarshals cleanly into an
		// empty slice. That used to reach the asker as a prompt with no question
		// in it and come back to the model as {"answers":[]}: the user, it seemed,
		// had nothing to say. Refuse instead, and say which shape is wanted.
		if qErr == nil {
			if len(req.Questions) == 0 {
				qErr = fmt.Errorf(`no questions to ask: the argument is {"questions": [{"question": "...", "options": ["..."]}]} — an array under "questions", even for one question`)
			}
			for _, item := range req.Questions {
				if strings.TrimSpace(item.Question) == "" {
					qErr = fmt.Errorf(`every entry in "questions" needs a non-empty "question"`)
					break
				}
			}
		}
		if qErr != nil || a.opts.QuestionAsker == nil {
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
			a.logQuestionAnswers(req.Questions, answers)
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
			Content:    `{"status":"not_supported","message":"plan_enter does not switch modes; this turn stays in its current mode. Planning runs in plan mode (orchestra apply --mode plan)."}`,
		})
		return serialToolOutcome{}, nil
	}

	if scopeErr := a.writeScopeRefusal(name, tc.Input); scopeErr != nil {
		toolResult := a.deniedToolResult(name, tc.Input, scopeErr.Error())
		*history = append(*history, llm.Message{Role: llm.RoleTool, ToolCallID: toolCallID, Content: toolResult})
		if cbErr := cb.RecordDenied(name); cbErr != nil {
			return serialToolOutcome{}, cbErr
		}
		return serialToolOutcome{}, nil
	}

	if gateErr := a.confirmHumanGate(ctx, name, tc.Input); gateErr != nil {
		toolResult := a.deniedToolResult(name, tc.Input, gateErr.Error())
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

	hookRewrite := ""
	if a.opts.HooksRunner != nil {
		dec := a.runPreToolHooks(callCtx, name, tc.Input)
		if dec.Denied {
			toolResult := a.deniedToolResult(name, tc.Input, hookDenialReason(dec))
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
		if len(dec.Input) > 0 {
			// The model asked for one thing and another ran. Everything after
			// this point — the tool call, the history entry, the write path —
			// uses what actually ran, and the model is told about the swap so
			// it does not read the result as an answer to its own arguments.
			tc.Input = dec.Input
			hookRewrite = hookRewriteNote(dec)
		}
	}

	if a.opts.AgentLogger != nil {
		a.opts.AgentLogger.LogToolCall(name, len(tc.Input), string(tc.Input))
	}

	if dedupExemptTool(name) {
		if cb.IsReadOnlyBlocked(name, tc.Input) {
			stopMsg := "⛔ The tool «" + name + "» was called too many times with identical arguments — the result is already in your history. Proceed with edit/write or use different arguments."
			a.logf("tool_call name=%s read_only_doom_blocked", name)
			a.recordBlocked(name, "read-only call repeated with identical arguments")
			*history = append(*history, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: toolCallID,
				Content:    stopMsg,
			})
			// A refusal the model ignores is the same signal as a denial it
			// ignores: it is not adapting. Counted on the same budget, so a
			// model stuck on one file ends the turn instead of re-asking
			// until MaxSteps with the answer already in its history.
			if cbErr := cb.RecordDenied(name); cbErr != nil {
				return serialToolOutcome{}, cbErr
			}
			return serialToolOutcome{}, nil
		}
	} else if cb.IsDuplicateCall(name, tc.Input) {
		stopMsg := duplicateCallRefusal(name)
		a.logf("tool_call name=%s dedup_blocked", name)
		a.recordBlocked(name, "duplicate call with identical arguments")
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
		// Same budget as a denial: a duplicate the model keeps re-issuing is a
		// stuck turn, not a recoverable step.
		if cbErr := cb.RecordDenied(name); cbErr != nil {
			return serialToolOutcome{}, cbErr
		}
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
			a.opts.AgentLogger.LogToolResult(name, 0, dur, err.Error(), "")
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
		a.opts.AgentLogger.LogToolResult(name, len(out), dur, "", string(out))
	}
	a.markExploreFirstSatisfied(name)
	*history = append(*history, llm.Message{
		Role:       llm.RoleTool,
		ToolCallID: toolCallID,
		Content:    hookRewrite + a.prepareToolHistoryContent(name, tc.Input, out),
	})

	// Counted here, on the success path, and not where the call is dispatched:
	// a write that was refused or failed changed nothing, and a turn whose
	// only mutating call errored must not look like a turn that did work.
	a.countMutatingTool(name)

	if name == "write" || name == "edit" {
		toolPath := extractWriteOrEditPath(tc.Input)
		// Recorded here rather than at the call site, because only a tool that
		// got this far actually changed the file. A failed edit followed by the
		// same change as a final patch is a legitimate recovery, and must not
		// be mistaken for a restatement of a change that already landed.
		a.recordMutatedPath(toolPath)
		a.commitStagedAfterMutatingTool(ctx, steps, toolPath)
		a.previewStagedAfterMutatingTool(ctx, steps)
		a.maybeHintStagedReady(history, toolPath)
		a.afterContractArtifactWrite(ctx, toolPath)
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

	// Same path for MCP servers that answer with pictures (Playwright-MCP and
	// any other visual server). The image goes in as its own user message
	// rather than inside the tool result: no provider accepts image content in
	// a tool-role message, and this is already how browser.screenshot works.
	if a.opts.MultimodalLLM && strings.HasPrefix(name, "mcp:") {
		if imgs := extractMCPImageParts(out); len(imgs) > 0 {
			parts := append([]llm.ContentPart{
				{Kind: llm.PartText, Text: "Image(s) returned by " + name + ":"},
			}, imgs...)
			*history = append(*history, llm.Message{Role: llm.RoleUser, Parts: parts})
		}
	}

	if name == "write" || name == "edit" {
		if hint := extractLSPErrors(out); hint != "" {
			path := extractWriteOrEditPath(tc.Input)
			streak := a.diags.Observe(path, fingerprintLSPErrors(out), hint)
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
			_ = a.diags.Observe(extractWriteOrEditPath(tc.Input), "", "")
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

func (a *Agent) requestInteractivePermission(ctx context.Context, toolName, subject string, input json.RawMessage) (bool, error) {
	if a.opts.PermissionRequester == nil {
		return false, fmt.Errorf("%s requires interactive approval (permission ask); connect TUI/IDE or use allow/deny rules", toolName)
	}
	desc := subject
	if desc == "" {
		desc = string(input)
	}
	desc = permissionText(desc)
	kind := toolName
	if toolName == "bash" {
		kind = "exec"
	}
	resp, err := a.opts.PermissionRequester.RequestPermission(ctx, PermissionRequest{
		Tool:        toolName,
		Kind:        kind,
		Description: desc,
	})
	if err != nil {
		return false, err
	}
	return resp.Approved, nil
}

// permissionMaxBytes bounds what one approval prompt shows. A command is
// approved as a whole, so the prompt shows it whole; it used to show the first
// 200 bytes, and a line with its payload past that point was approved by the
// harmless start.
const permissionMaxBytes = 16 * 1024

// permissionText is what the user approves. Past permissionMaxBytes it says,
// in the text itself, how much is not shown, and never cuts a UTF-8 sequence.
func permissionText(s string) string {
	if len(s) <= permissionMaxBytes {
		return s
	}
	cut := permissionMaxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + fmt.Sprintf("\n…[%d more bytes not shown — deny unless you know what they are]", len(s)-cut)
}
