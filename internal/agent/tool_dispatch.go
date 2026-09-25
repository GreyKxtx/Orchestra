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

// runSerialToolCall executes one tool call: the gate chain (toolGates), then
// the agent's own handler for an in-process tool (inProcessTools) or
// tools.Runner for the rest. Appends tool messages to history. Returns a
// non-nil error for circuit-breaker trips; EarlyResult when the run should end
// (task_result child, plan_exit approved).
func (a *Agent) runSerialToolCall(ctx context.Context, cb *CircuitBreaker, history *[]llm.Message, tc ToolCall, steps int, emitStepDone func(string)) (serialToolOutcome, error) {
	name := normalizeToolName(tc.Name)
	if name == "" {
		return serialToolOutcome{}, nil
	}
	toolCallID := strings.TrimSpace(tc.ID)
	if toolCallID == "" {
		toolCallID = fmt.Sprintf("call_%d_%d", steps, time.Now().UnixNano())
	}
	gc := a.newGateCall(name, tc.Input)
	if reason := a.runToolGates(ctx, gc, *history); reason != "" {
		return a.refuseCall(cb, history, toolCallID, gc, reason)
	}
	if h, ok := a.inProcessHandler(name); ok {
		return a.runInProcessTool(ctx, cb, history, inProcessCall{id: toolCallID, name: name, input: tc.Input, step: steps}, h, emitStepDone)
	}
	return a.runRunnerTool(ctx, cb, history, tc, name, toolCallID, steps)
}

// runRunnerTool runs a gated call through tools.Runner: the PreTool hooks,
// the repeat guards, the call, and what a successful result sets off.
func (a *Agent) runRunnerTool(ctx context.Context, cb *CircuitBreaker, history *[]llm.Message, tc ToolCall, name, toolCallID string, steps int) (serialToolOutcome, error) {
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
			return a.refuseCall(cb, history, toolCallID, a.newGateCall(name, tc.Input), hookDenialReason(dec))
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

	if blocked, cbErr := a.repeatRefusal(cb, history, tc, name, toolCallID, steps); blocked {
		return serialToolOutcome{}, cbErr
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

	a.afterRunnerSuccess(ctx, callCtx, cb, history, tc, name, toolCallID, steps, out, dur, hookRewrite)
	return serialToolOutcome{}, nil
}

// repeatRefusal stops a call the model keeps repeating with identical
// arguments: a read-only call past its budget, or a duplicate of a mutating
// call. blocked reports that the call was answered here; cbErr is a breaker
// trip.
func (a *Agent) repeatRefusal(cb *CircuitBreaker, history *[]llm.Message, tc ToolCall, name, toolCallID string, steps int) (blocked bool, cbErr error) {
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
			return true, breakerErr(cb.RecordDenied(name))
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
		return true, breakerErr(cb.RecordDenied(name))
	}
	return false, nil
}

// afterRunnerSuccess records a successful Runner call and does what it sets
// off: PostTool hooks, the history entry, staging and LSP feedback for a write
// or edit, images for the multimodal model, and the repeat bookkeeping.
func (a *Agent) afterRunnerSuccess(ctx, callCtx context.Context, cb *CircuitBreaker, history *[]llm.Message, tc ToolCall, name, toolCallID string, steps int, out []byte, dur int64, hookRewrite string) {
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
