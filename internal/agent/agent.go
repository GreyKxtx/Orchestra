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

	"github.com/orchestra/orchestra/internal/plan"
	promptpkg "github.com/orchestra/orchestra/internal/prompt"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/tools"

	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/memory"
)

// Run executes the agent loop for userQuery and returns the updated history, result, and error.
// Pass a nil history for a fresh (one-shot) run; pass an existing history to continue a session.
func (a *Agent) Run(ctx context.Context, history []llm.Message, userQuery string) ([]llm.Message, *Result, error) {
	userQuery = strings.TrimSpace(userQuery)
	if userQuery == "" {
		return nil, nil, fmt.Errorf("user query is empty")
	}
	// Initialize todos from session state (empty for one-shot runs).
	a.todos = append([]tools.TodoItem(nil), a.opts.InitialTodos...)
	a.turnMutatingTools = 0
	a.overflowRecoveries = 0
	a.contextPressureWarned = false
	a.initWorkingState(userQuery)
	defer a.persistWorkingTurnDigest()
	// Pre-fetch relevant CKG nodes once per Run (injected only on step 1).
	a.ckgContext = a.tools.FetchCKGContext(ctx, userQuery)

	if history == nil {
		history = make([]llm.Message, 0, 32)
	}
	steps := 0
	a.syncModelContextFromClient()
	maxStepsReminderSent := false
	cb := NewCircuitBreaker(a.opts.MaxDeniedToolRepeats, a.opts.MaxToolErrorRepeats, a.opts.MaxFinalFailures, a.opts.MaxInvalidRetries)
	cb.ResetDedup()

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
			var protect []string
			if a.working != nil {
				protect = a.working.ActiveFiles()
			}
			history = pruneRetroactiveToolHistory(history, a.opts.ToolDigestBytes, keep, protect...)
		}

		// Soft notice once when context approaches the compact threshold
		// (warnPct ≈ compactPct−5, floor 50%) so TUI can hint before a full compact.
		if !a.contextPressureWarned && a.opts.CompactThresholdPct > 0 && a.opts.MaxPromptBytes > 0 {
			warnPct := a.opts.CompactThresholdPct - 5
			if warnPct < 50 {
				warnPct = a.opts.CompactThresholdPct * 3 / 4
			}
			if warnPct > 0 && warnPct < a.opts.CompactThresholdPct && shouldCompactHistoryEx(
				history,
				a.opts.MaxPromptBytes,
				warnPct,
				a.lastPromptTokens,
				a.opts.ModelContextTokens,
				a.opts.CompletionMaxTokens,
				a.bytesPerToken(),
			) {
				a.contextPressureWarned = true
				if a.opts.OnEvent != nil {
					a.opts.OnEvent(AgentEvent{Step: steps, Stream: llm.StreamEvent{
						Kind:    llm.StreamEventRecoverableError,
						Content: "CONTEXT_PRESSURE",
					}})
				}
			}
		}

		// Compaction: if history is getting large, summarise it before the next LLM call.
		// Fires only at the top of the loop so history is always in a consistent state
		// (no orphaned tool_calls without tool_results).
		//
		// H15 in audit ledger: convergence guard. The previous loop kept calling
		// compactHistory every step when the summary itself still exceeded the
		// threshold вЂ” each step burning an LLM call to produce a slightly larger
		// summary-of-summary until MaxSteps tripped. We now (a) refuse to use a
		// "compacted" result that didn't actually shrink history by at least 20%,
		// and (b) fall back to plain truncateMessages when compaction declines
		// to converge, breaking the loop on the very next step.
		if a.opts.CompactThresholdPct > 0 && a.opts.MaxPromptBytes > 0 {
			force := a.opts.ForceCompactOnce
			a.opts.ForceCompactOnce = false
			need := force || shouldCompactHistoryEx(
				history,
				a.opts.MaxPromptBytes,
				a.opts.CompactThresholdPct,
				a.lastPromptTokens,
				a.opts.ModelContextTokens,
				a.opts.CompletionMaxTokens,
				a.bytesPerToken(),
			)
			if need {
				before := historyBytes(history)
				compacted, compactErr := a.compactHistory(ctx, userQuery, history)
				if compactErr != nil {
					a.logf("compaction failed (non-fatal), continuing with truncation: %v", compactErr)
					history = truncateMessages(history, a.opts.MaxPromptBytes)
					a.recordCompactMetrics(before, historyBytes(history), false)
				} else {
					after := historyBytes(compacted)
					if after*5 >= before*4 { // < 20% shrink
						a.logf("compaction did not converge: %d → %d bytes (≥80%% retained); falling back to truncation", before, after)
						history = truncateMessages(history, a.opts.MaxPromptBytes)
						a.recordCompactMetrics(before, historyBytes(history), false)
					} else {
						a.logf("history compacted: %d bytes → %d bytes", before, after)
						history = compacted
						a.recordCompactMetrics(before, after, true)
						cb.ResetReadOnlyCalls()
						if a.opts.OnEvent != nil {
							a.opts.OnEvent(AgentEvent{Step: steps, Stream: llm.StreamEvent{
								Kind:    llm.StreamEventRecoverableError,
								Content: "CONTEXT_COMPACTED",
							}})
						}
					}
				}
				if afterBytes := historyBytes(history); afterBytes < before {
					a.emitPromptContextEstimate(steps, history)
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

		// Refresh TUI ctx bar every step. Local providers (LM Studio) often omit
		// stream usage; the estimate keeps prompt_ctx / status bar non-zero.
		// Real usage from Done/emitStepUsage overwrites when the provider reports it.
		a.emitPromptContextEstimate(steps, history)

		step, raw, llmResp, err := a.nextStep(ctx, userQuery, history, steps)
		if err != nil {
			// Provider rejected the request because prompt + max_tokens exceeds
			// the model window. Compact and replay the step (OpenCode-style)
			// instead of failing the turn.
			if ctx.Err() == nil && llm.IsContextOverflowError(err) {
				if shrunk, ok := a.recoverFromOverflow(ctx, userQuery, history, err, steps); ok {
					history = shrunk
					steps--
					continue
				}
			}
			return history, nil, err
		}
		if step == nil {
			return history, nil, fmt.Errorf("nextStep returned nil step without error")
		}
		// resp can be nil only if there was an error, which should have been returned above
		// But we handle it gracefully for tool calls that need tool_call_id

		switch step.Type {
		case StepToolCall:
			calls := a.resolveToolCalls(step, llmResp)
			if len(calls) == 0 {
				if cbErr := cb.RecordInvalid(); cbErr != nil {
					return history, nil, cbErr
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
					return history, nil, cbErr
				}
				emitStepDone("tool_call")
				a.maybePersistMicroDigest(steps)
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
					return history, nil, err
				}
				if outcome.EarlyResult != nil {
					return history, outcome.EarlyResult, nil
				}
			}
			emitStepDone("tool_call")
			a.maybePersistMicroDigest(steps)
			continue

		case StepFinal:
			if hint, reject := a.rejectPrematureFinal(userQuery, step, raw, steps); reject {
				if cbErr := cb.RecordInvalid(); cbErr != nil {
					return history, nil, cbErr
				}
				history = append(history, llm.Message{
					Role:    llm.RoleUser,
					Content: formatValidatorErrorCompact(hint),
				})
				// Empty-response retries stay in the agent loop; skip TUI spam.
				if a.opts.OnEvent != nil && !isSilentPrematureFinalHint(hint) {
					a.opts.OnEvent(AgentEvent{Step: steps, Stream: llm.StreamEvent{
						Kind:    llm.StreamEventRecoverableError,
						Content: hint,
					}})
				}
				emitStepDone("invalid")
				continue
			}
			outcome, err := a.handleFinalStep(ctx, cb, &history, step, llmResp, steps, raw, emitStepDone)
			if err != nil {
				return history, nil, err
			}
			if outcome.Retry {
				continue
			}
			return history, outcome.Result, nil

		default:
			// M7+M8 in audit ledger: unknown step type counts toward the
			// MaxInvalidRetries cap so a model emitting persistently bogus
			// step shapes can't loop forever within MaxSteps.
			if cbErr := cb.RecordInvalid(); cbErr != nil {
				return history, nil, cbErr
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
	// Soft-stop: always return history so SessionMessage can persist it.
	// A hard error here used to leave UI chat on disk but empty agent history —
	// reopen then forced the model to re-read everything.
	if a.opts.OnEvent != nil {
		a.opts.OnEvent(AgentEvent{Step: steps, Stream: llm.StreamEvent{
			Kind:    llm.StreamEventRecoverableError,
			Content: "MAX_STEPS — история сохранена, продолжите новым сообщением",
		}})
	}
	return history, &Result{
		Steps:            steps,
		Todos:            a.todos,
		MaxStepsExceeded: true,
		StopReason:       "max_steps",
	}, nil
}

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
// step. The pipeline has five distinct stages вЂ” the first that fires
// becomes the *base*, the rest *append* on top:
//
//  1. BASE candidate: promptpkg.BuildSystemPromptForMode(Mode, PromptFamily)
//     вЂ” the built-in prompt for the agent's mode.
//  2. BASE override: Options.SystemPromptOverride
//     вЂ” when a custom agent declares a system_prompt in .orchestra.yml,
//     it REPLACES the mode default.
//  3. BASE override (highest precedence): .orchestra/system.txt in the
//     workspace root, loaded via promptpkg.LoadSystemOverride. If this
//     file exists it REPLACES whatever was selected above, including the
//     custom-agent prompt вЂ” file-system wins over config.
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
	// 1+2+3: base вЂ” first non-empty replacement wins (.orchestra/system.txt
	// > Options.SystemPromptOverride > mode default).
	prompt := promptpkg.BuildSystemPromptForMode(string(a.opts.Mode), a.opts.PromptFamily)
	if a.opts.SystemPromptOverride != "" {
		prompt = a.opts.SystemPromptOverride
	}
	if fs := promptpkg.LoadSystemOverride(a.tools.WorkspaceRoot()); fs != "" {
		prompt = fs
	}
	// 4: append project memory (tiered, config-driven).
	// Workers/focused children skip this — they only need the WorkOrder.
	if !a.opts.SkipMemoryInject && a.opts.Mode != ModeWorker {
		memCfg := a.opts.Memory
		memCfg.Normalize()
		store := memory.NewStore(a.tools.WorkspaceRoot(), a.opts.SessionID, memCfg)
		if block := store.FormatInject(memCfg.InjectBytes()); block != "" {
			prompt += "\n\n" + block
		}
	}
	// 5: live tool catalog (mode/caps accurate вЂ” better than hardcoded lists in *.txt).
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
		fmt.Fprintf(&b, "- %s вЂ” %s\n", s.Name, s.Description)
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
	if block := a.injectWorkingPromptBlocks(); block != "" {
		userPrompt += "\n\n" + block
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

	// Track the request size so real Usage.PromptTokens can calibrate our
	// bytes-per-token heuristic (see calibrateFromRealPrompt).
	a.lastPromptBytes = messagesBytes(messages) + toolDefsBytes(toolDefs)

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
		var resp *llm.CompleteResponse
		var err error
		if canStream && a.opts.OnEvent != nil {
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
			// Surface LLMStepTimeout clearly — raw "SSE read error: context
			// deadline exceeded" hides that llm.timeout_s / LLMStepTimeout fired.
			if stepDeadlineExceeded && ctx.Err() == nil && a.opts.LLMStepTimeout > 0 {
				sec := int(a.opts.LLMStepTimeout / time.Second)
				return nil, "", nil, fmt.Errorf(
					"LLM step timed out after %s (llm.timeout_s=%d; raise it and restart core): %w",
					a.opts.LLMStepTimeout.Round(time.Second), sec, err)
			}
			return nil, "", nil, err
		}
		if !canStream || a.opts.OnEvent == nil {
			a.emitStepUsage(stepNum, resp)
		}
		a.mergeResponsePrefill(resp)
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
		// Content already streamed to the UI: retrying would duplicate it and
		// diverge from what the user saw — surface the error instead.
		if ctx.Err() != nil || contentStarted || !llm.IsTransientLLMError(err) || attempt == maxStreamAttempts {
			// Context overflow is handled by the Run loop (compact + replay);
			// emitting a hard error here would show the user a failure for a
			// step that is about to be retried successfully.
			if a.opts.OnEvent != nil && !llm.IsContextOverflowError(err) {
				a.opts.OnEvent(AgentEvent{Step: step, Stream: llm.StreamEvent{
					Kind: llm.StreamEventError,
					Err:  err,
				}})
			}
			return nil, err
		}
		a.logf("stream attempt %d/%d failed (transient): %v — retrying", attempt, maxStreamAttempts, err)
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

func (a *Agent) emitStepUsage(step int, resp *llm.CompleteResponse) {
	if resp != nil && resp.Usage != nil && resp.Usage.PromptTokens > 0 {
		a.lastPromptTokens = resp.Usage.PromptTokens
		a.calibrateFromRealPrompt(resp.Usage.PromptTokens)
	}
	if a.opts.OnEvent == nil || resp == nil || resp.Usage == nil {
		return
	}
	u := resp.Usage
	payload, _ := json.Marshal(map[string]int{
		"prompt_tokens":     u.PromptTokens,
		"completion_tokens": u.CompletionTokens,
		"total_tokens":      u.TotalTokens,
	})
	a.opts.OnEvent(AgentEvent{Step: step, Stream: llm.StreamEvent{
		Kind:    llm.StreamEventStepUsage,
		Content: string(payload),
	}})
}

// bytesPerToken returns the calibration factor for token estimates. A value
// learned from real provider usage wins over config when it is more
// pessimistic — under-estimating the prompt is what triggers 400s.
func (a *Agent) bytesPerToken() int {
	if a == nil {
		return DefaultBytesPerContextToken
	}
	base := a.opts.BytesPerContextToken
	if base <= 0 {
		base = DefaultBytesPerContextToken
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
		return a.substitutePlanPath("Architecture mode: design only — write plans under {{PLAN_PATH}}; no production edits.")
	case ModeAsk:
		return "Ask mode: read-only answers. Do not edit code."
	case ModeDebug:
		return "Debug mode: find root cause with evidence; fix narrowly or delegate worker."
	case ModeOrchestra:
		return "You are Orchestra Lead: plan and delegate via task(subagent_type=worker|ask|debug|architecture|explore, tier=…). Do not edit production code."
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
				results[idx] = a.prepareToolHistoryContent(call.Name, call.Input, out)
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
