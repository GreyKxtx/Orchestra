package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol"
)

// execCommandFromInput extracts the command basename from exec.run JSON input.
func execCommandFromInput(input json.RawMessage) string {
	var req struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(input, &req)
	return req.Command
}

// execCommandAllowed reports whether cmd is permitted by the allow/deny lists.
// Deny takes precedence. Empty allow list with no deny list в†’ deny all.
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
		// Structured detail (the resolver's nearest-region excerpt, ambiguous
		// match line numbers, …) used to be dropped here, so the model saw
		// "search block not found" and nothing it could act on — its only
		// recovery was to re-read the whole file and guess again.
		if d := compactErrorDetails(pe.Data); d != nil {
			result["details"] = d
		}
	} else {
		result["error"] = err.Error()
	}
	b, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf(`{"status":"error","tool":"%s","error":"%s"}`, name, err.Error())
	}
	return string(b)
}

// toolErrorDetailMaxChars caps one detail value pasted back into history.
const toolErrorDetailMaxChars = 2000

// compactErrorDetails prepares protocol error data for the model: string values
// are clipped so a large excerpt cannot dominate the history, and empty values
// are dropped.
func compactErrorDetails(data any) any {
	m, ok := data.(map[string]any)
	if !ok {
		return data
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		switch tv := v.(type) {
		case nil:
			continue
		case string:
			if tv == "" {
				continue
			}
			out[k] = truncate(tv, toolErrorDetailMaxChars)
		default:
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
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
// Only ParallelSafe tools reach this path вЂ” see NormalizeLLMWithDefs. The
// rich per-tool serial pipeline (exec consent, plan-mode write guard, todo
// dispatcher, вЂ¦) is therefore not exercised here; we go straight through
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
	// denied[i] is true when the pre-tool hook rejected call i вЂ” we'll skip
	// the parallel tool execution for that index and just emit the denial.
	denied := make([]bool, len(calls))
	// errored[i] is true when a.tools.Call returned a non-nil error for call i.
	// Used after wg.Wait to drive circuit-breaker bookkeeping in order.
	errored := make([]bool, len(calls))

	// 2) PreTool hooks: run serially. Most hooks aren't reentrant (they spawn
	//    a subprocess that writes to a shared log file). Spawning 16 of them
	//    concurrently turns each call into a 5-second timeout because of
	//    file-lock contention and OS process-startup pressure.
	// rewrote[i] is the note prefixed to call i's result when a hook replaced
	// its input, so the model does not read the result as an answer to the
	// arguments it actually sent.
	rewrote := make([]string, len(calls))
	if a.opts.HooksRunner != nil {
		for i, call := range calls {
			dec := a.runPreToolHooks(ctx, call.Name, call.Input)
			if !dec.Denied && len(dec.Input) > 0 {
				// The fan-out below re-ranges over calls, so the rewritten
				// input is what actually runs.
				calls[i].Input = dec.Input
				rewrote[i] = hookRewriteNote(dec)
			}
			if dec.Denied {
				denied[i] = true
				results[i] = formatToolDeniedJSON(call.Name, call.Input, hookDenialReason(dec))
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
			results[i] = "в›” STOP. The tool В«" + tc.Name + "В» was called too many times with identical arguments вЂ” proceed with a different tool or final answer."
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
					a.observeWorkingTool(call.Name, call.Input, out, callErr)
					return callErr
				}
				results[idx] = rewrote[idx] + a.prepareToolHistoryContent(call.Name, call.Input, out)
				if a.opts.OnEvent != nil {
					_ = safeRun("OnEvent ToolCallCompleted", func() {
						a.opts.OnEvent(AgentEvent{Step: stepNum, Stream: toolCallCompletedStreamEvent(call.Name, call.ID, out, nil)})
					})
				}
				return nil
			})
			if err != nil {
				errored[idx] = true
				results[idx] = formatToolErrorJSON(call.Name, call.Input, err)
				if a.opts.OnEvent != nil {
					_ = safeRun("OnEvent ToolCallCompleted (err)", func() {
						a.opts.OnEvent(AgentEvent{Step: stepNum, Stream: toolCallCompletedStreamEvent(call.Name, call.ID, nil, err)})
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

	// 5) Circuit-breaker bookkeeping вЂ” runs single-threaded after fan-out so
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
