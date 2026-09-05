package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	promptpkg "github.com/orchestra/orchestra/internal/prompt"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/protocol"

	"github.com/orchestra/orchestra/llm"
)

// nextStep returns the next step, raw response, full LLM response, and error.
// stepNum is the current step count (used for streaming event tagging).
func (a *Agent) nextStep(ctx context.Context, userQuery string, history []llm.Message, stepNum int) (*Step, string, *llm.CompleteResponse, error) {
	toolDefs := a.buildToolDefs()
	systemPrompt := a.buildSystemPrompt()
	snap := promptpkg.BuildUserInfoSnapshot(a.tools.WorkspaceRoot())
	userPrompt := promptpkg.BuildUserPrompt(userQuery, snap, tools.ToolNames(toolDefs))
	// CKG context only on step 1 (saves tokens; later steps use explore/grep in history).
	if stepNum == 1 && a.ckgContext != "" {
		userPrompt += "\n\n" + a.ckgContext
	}

	// Volatile context — todos, working state, turn digests, mode reminder —
	// goes AFTER the history, not into the leading user message.
	//
	// These blocks change on nearly every step (working state is updated after
	// each tool call). In front of the history they broke the common prefix at
	// message #2, so no provider prompt cache could match anything beyond the
	// system block and every step re-paid for the whole transcript. Appended
	// last they leave a stable, append-only prefix — and land in the freshest
	// part of the attention window, which is where a reminder belongs anyway.
	var volatileParts []string
	if block := renderTodosBlock(a.todos); block != "" {
		volatileParts = append(volatileParts, block)
	}
	if block := a.injectWorkingPromptBlocks(); block != "" {
		volatileParts = append(volatileParts, block)
	}
	if reminder := a.modeReminder(); reminder != "" {
		volatileParts = append(volatileParts, reminder)
	}
	volatileBlock := strings.Join(volatileParts, "\n\n")

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

	// Truncate messages if needed to stay within budget (best-effort).
	// The volatile tail is appended after truncation, so reserve its size here.
	if a.opts.MaxPromptBytes > 0 {
		budget := a.opts.MaxPromptBytes - len(volatileBlock)
		if budget < a.opts.MaxPromptBytes/2 {
			budget = a.opts.MaxPromptBytes / 2
		}
		beforeTruncate := len(messages)
		messages = truncateMessages(messages, budget)
		if a.opts.Debug && len(messages) != beforeTruncate {
			a.logf("agent.nextStep messages truncated: %d -> %d (budget=%d)", beforeTruncate, len(messages), budget)
		}
	}
	if volatileBlock != "" {
		messages = append(messages, llm.Message{Role: llm.RoleUser, Content: volatileBlock})
	}

	// Track the request size so real Usage.PromptTokens can calibrate our
	// bytes-per-token heuristic (see calibrateFromRealPrompt).
	a.lastPromptBytes = messagesBytes(messages) + toolDefsBytes(toolDefs)

	// Until the provider reports real usage, estimate bytes-per-token from the
	// script of the prompt itself: a Cyrillic/CJK prompt costs fewer bytes per
	// token than the Latin-shaped default assumes, and under-counting tokens is
	// the direction that ends in a context-length 400.
	if a.detectedBytesPerToken == 0 {
		a.detectedBytesPerToken = detectBytesPerToken(sampleMessagesText(messages))
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
			Messages:       a.messagesWithAssistantPrefill(messages),
			Tools:          toolDefs,
			ResponseFormat: a.opts.ResponseFormat,
		}
		if ctxTok := a.opts.ModelContextTokens; ctxTok > 0 {
			est := llm.EstimateCompleteRequestTokens(llmReq)
			if est >= ctxTok {
				a.logf("warning: estimated prompt ~%d tokens exceeds model context %d; raise Context Length in LM Studio / extra_body.num_ctx", est, ctxTok)
				if stepNum == 1 && len(history) == 0 && est >= ctxTok-256 {
					if cancel != nil {
						cancel()
					}
					return nil, "", nil, fmt.Errorf(
						"prompt too large (~%d tokens) for model context %d — raise Context Length (num_ctx) in LM Studio / .orchestra.yml extra_body.num_ctx",
						est, ctxTok)
				}
			}
		}
		var resp *llm.CompleteResponse
		var err error
		if canStream {
			resp, err = a.streamStep(stepCtx, llmReq, streamer, stepNum)
		} else {
			resp, err = a.llm.Complete(stepCtx, llmReq)
		}
		// Snapshot before cancel(): after Cancel, a WithTimeout context that has
		// not yet expired reports context.Canceled, hiding a real deadline miss.
		stepDeadlineExceeded := stepCtx.Err() == context.DeadlineExceeded
		if cancel != nil {
			cancel() // Always cancel timeout context to free resources
		}
		if err != nil {
			if llm.IsUnreachableError(err) {
				a.llmInfraErr = err
			}
			// Surface LLMStepTimeout clearly  -  raw "SSE read error: context
			// deadline exceeded" hides that llm.timeout_s / LLMStepTimeout fired.
			if stepDeadlineExceeded && ctx.Err() == nil && a.opts.LLMStepTimeout > 0 {
				sec := int(a.opts.LLMStepTimeout / time.Second)
				return nil, "", nil, fmt.Errorf(
					"LLM step timed out after %s (llm.timeout_s=%d; raise it and restart core): %w",
					a.opts.LLMStepTimeout.Round(time.Second), sec, err)
			}
			return nil, "", nil, err
		}
		// Emit on both paths. The streaming path used to rely on core
		// translating StreamEventDone into a step_usage notification, which
		// rebuilt the payload by hand and dropped everything it did not know
		// about — the prompt-cache counters among them, on the path every
		// interactive run takes.
		a.emitStepUsage(stepNum, resp)
		a.mergeResponsePrefill(resp)
		if a.opts.UsageTracker != nil && resp != nil {
			if resp.Usage != nil {
				a.recordUsage(resp.Usage)
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

// streamStep calls CompleteStream and forwards events to OnEvent, returning
// the final assembled CompleteResponse from the Done event. Transient stream
// failures (dead tunnel, stall, reset) before any assistant content arrived
// are retried in place so one network hiccup doesn't kill a long agent turn.
func (a *Agent) streamStep(ctx context.Context, req llm.CompleteRequest, s llm.Streamer, step int) (*llm.CompleteResponse, error) {
	const maxStreamAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= maxStreamAttempts; attempt++ {
		resp, contentStarted, err := a.streamStepOnce(ctx, req, s, step)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if llm.IsUnreachableError(err) {
			a.llmInfraErr = err
			if a.opts.OnEvent != nil {
				a.opts.OnEvent(AgentEvent{Step: step, Stream: llm.StreamEvent{
					Kind: llm.StreamEventError,
					Err:  err,
				}})
			}
			return nil, err
		}
		// Content already streamed to the UI: retrying would duplicate it and
		// diverge from what the user saw  -  surface the error instead.
		if ctx.Err() != nil || contentStarted || !llm.IsTransientLLMError(err) || attempt == maxStreamAttempts {
			// Context overflow is handled by the Run loop (compact + replay);
			// emitting a hard error here would show the user a failure for a
			// step that is about to be retried successfully.
			if a.opts.OnEvent != nil && !llm.IsContextOverflowError(err) && !isBenignCancelErr(ctx, err) {
				a.opts.OnEvent(AgentEvent{Step: step, Stream: llm.StreamEvent{
					Kind: llm.StreamEventError,
					Err:  err,
				}})
			}
			return nil, err
		}
		a.logf("stream attempt %d/%d failed (transient): %v  -  retrying", attempt, maxStreamAttempts, err)
		if a.opts.OnEvent != nil {
			a.opts.OnEvent(AgentEvent{Step: step, Stream: llm.StreamEvent{
				Kind:    llm.StreamEventRecoverableError,
				Content: truncate(fmt.Sprintf("LLM stream interrupted, retry %d/%d: %v", attempt, maxStreamAttempts-1, err), 200),
			}})
		}
		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(time.Duration(attempt) * 2 * time.Second):
		}
	}
	return nil, lastErr
}

// streamStepOnce runs one streaming attempt. contentStarted reports whether
// any assistant text or tool-call delta was already forwarded to OnEvent.
func (a *Agent) streamStepOnce(ctx context.Context, req llm.CompleteRequest, s llm.Streamer, step int) (*llm.CompleteResponse, bool, error) {
	contentStarted := false
	ch, err := s.CompleteStream(ctx, req)
	if err != nil {
		return nil, contentStarted, err
	}
	var final *llm.CompleteResponse
	for ev := range ch {
		switch ev.Kind {
		case llm.StreamEventMessageDelta, llm.StreamEventToolCallStart, llm.StreamEventToolCallDelta:
			contentStarted = true
		}
		// Error events are not forwarded here: streamStep decides whether to
		// retry silently (recoverable notice) or surface the failure.
		if ev.Kind == llm.StreamEventError {
			// Drain remaining events so the producer goroutine can exit.
			for range ch {
			}
			return nil, contentStarted, ev.Err
		}
		if a.opts.OnEvent != nil {
			a.opts.OnEvent(AgentEvent{Step: step, Stream: ev})
		}
		if ev.Kind == llm.StreamEventDone {
			final = ev.Response
			// Streaming path: OnEvent already forwarded usage to the UI, but the
			// agent's own budgeting state must be updated too, otherwise the
			// usage-based compaction trigger never fires in the TUI.
			if final != nil && final.Usage != nil && final.Usage.PromptTokens > 0 {
				a.lastPromptTokens = final.Usage.PromptTokens
				a.calibrateFromRealPrompt(final.Usage.PromptTokens)
			}
		}
	}
	if final == nil {
		return nil, contentStarted, fmt.Errorf("stream ended without Done event")
	}
	return final, contentStarted, nil
}

// recordUsage hands one completion's accounting to the usage recorder: the
// gross tokens and cost always, the prompt-cache split when the recorder can
// take it. Without the split, usage.jsonl could not tell a run that re-billed
// its whole transcript every step from one that cached it — the field run's
// most expensive turn was exactly that question, unanswerable after the fact.
func (a *Agent) recordUsage(u *llm.TokenUsage) {
	if a.opts.UsageTracker == nil || u == nil {
		return
	}
	provider := a.providerLabel()
	a.opts.UsageTracker.RecordCost(provider, a.opts.ModelLabel,
		u.PromptTokens, u.CompletionTokens, u.CostUSD)
	if pc, ok := a.opts.UsageTracker.(PromptCacheRecorder); ok {
		pc.RecordPromptCache(provider, a.opts.ModelLabel, u.CachedPromptTokens, u.CacheWriteTokens)
	}
}

// providerLabel names the provider that answered. It is the configured label
// unless the client failed over, in which case the standby's name is what
// belongs in the ledger — attributing a fallback's spend to a provider that
// was down makes usage.jsonl lie in both directions at once.
func (a *Agent) providerLabel() string {
	if a.activeProvider != nil {
		if name := strings.TrimSpace(a.activeProvider.ActiveProvider()); name != "" {
			return name
		}
	}
	return a.opts.ProviderLabel
}

func (a *Agent) emitStepUsage(step int, resp *llm.CompleteResponse) {
	if resp != nil && resp.Usage != nil && resp.Usage.PromptTokens > 0 {
		a.lastPromptTokens = resp.Usage.PromptTokens
		a.calibrateFromRealPrompt(resp.Usage.PromptTokens)
	}
	if a.opts.OnEvent == nil || resp == nil || resp.Usage == nil {
		return
	}
	u := resp.Usage
	payload, _ := json.Marshal(map[string]any{
		// cached_prompt_tokens is the prompt-cache hit. On a long run it should
		// grow with the history; a run that keeps it at 0 is re-billing the whole
		// transcript every step (see markPrefixCacheBreakpoint in llm/anthropic.go).
		"cached_prompt_tokens": u.CachedPromptTokens,
		"cache_write_tokens":   u.CacheWriteTokens,
		"prompt_tokens":        u.PromptTokens,
		"completion_tokens":    u.CompletionTokens,
		"total_tokens":         u.TotalTokens,
		"cost_usd":             u.CostUSD,
	})
	a.opts.OnEvent(AgentEvent{Step: step, Stream: llm.StreamEvent{
		Kind:    llm.StreamEventStepUsage,
		Content: string(payload),
	}})
}

// bytesPerToken returns the calibration factor for token estimates. A value
// learned from real provider usage wins over config when it is more
// pessimistic  -  under-estimating the prompt is what triggers 400s.
func (a *Agent) bytesPerToken() int {
	if a == nil {
		return DefaultBytesPerContextToken
	}
	base := a.opts.BytesPerContextToken
	if base <= 0 {
		base = DefaultBytesPerContextToken
	}
	// Before the provider reports anything, fall back to the script-based
	// guess when it is more pessimistic than the configured default.
	if d := a.detectedBytesPerToken; d > 0 && d < base {
		base = d
	}
	if c := a.calibratedBytesPerToken; c > 0 && c < base {
		return c
	}
	return base
}

// messagesBytes approximates the serialized size of a request's messages.
func messagesBytes(msgs []llm.Message) int {
	total := 0
	for _, m := range msgs {
		total += estimateMessageSize(m)
	}
	return total
}

// toolDefsBytes approximates the serialized size of advertised tool schemas,
// which count against the model window just like messages do.
func toolDefsBytes(defs []llm.ToolDef) int {
	total := 0
	for _, d := range defs {
		total += len(d.Function.Name) + len(d.Function.Description) + len(d.Function.Parameters)
	}
	return total
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

// modeReminder returns the reminder string to append to the user prompt for the current mode.
// The build-switch reminder fires at most once (cleared after the first call).
func (a *Agent) modeReminder() string {
	switch a.opts.Mode {
	case ModePlan:
		return a.substitutePlanPath(promptpkg.PlanModeReminder)
	case ModeArchitecture:
		return a.substitutePlanPath("Architecture mode: design only  -  write plans under {{PLAN_PATH}}; no production edits.")
	case ModeAsk:
		return "Ask mode: read-only answers. Do not edit code."
	case ModeDebug:
		return "Debug mode: find root cause with evidence; fix narrowly or delegate worker."
	case ModeVerifier:
		return "Verifier mode: goal-backward read-only checks; finish with ## VERIFICATION PASSED or ## VERIFICATION FAILED."
	case ModeOrchestra:
		return "You are Orchestra Lead: plan and delegate via task(subagent_type=worker|verifier|ask|debug|architecture|explore, tier=complex|focused|micro). Do not edit production code."
	case ModeBuild, "":
		if a.justSwitchedFromPlan {
			a.justSwitchedFromPlan = false
			return a.substitutePlanPath(promptpkg.BuildSwitchReminder)
		}
	}
	return ""
}

func isBenignCancelErr(ctx context.Context, err error) bool {
	if err == nil {
		return ctx.Err() != nil
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "context canceled") || strings.Contains(msg, "context cancelled") {
		return true
	}
	if strings.Contains(msg, "sse read error") && strings.Contains(msg, "cancel") {
		return true
	}
	return ctx.Err() != nil && errors.Is(ctx.Err(), context.Canceled)
}
