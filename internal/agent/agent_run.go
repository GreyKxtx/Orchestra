package agent

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"

	promptpkg "github.com/orchestra/orchestra/internal/prompt"
	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/internal/tools"

	"github.com/orchestra/orchestra/llm"
)

// Run executes the agent loop for userQuery and returns the updated history, result, and error.
// Pass a nil history for a fresh (one-shot) run; pass an existing history to continue a session.
//
// Panic containment (resilience audit P1): tool calls already convert panics
// to errors inside tools.Runner.Call, but the loop itself (compaction,
// resolver, prompt assembly) did not. A panic here inside a child-agent
// goroutine would crash the entire core process — parent orchestrator,
// sibling workers and the RPC server included. Recover at the boundary and
// surface it as a regular error instead.
func (a *Agent) Run(ctx context.Context, history []llm.Message, userQuery string) (outHistory []llm.Message, result *Result, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			// Best effort: return the history we started with so the caller
			// can still persist the session up to the last completed turn.
			outHistory = history
			result = nil
			err = fmt.Errorf("agent run panicked: %v\n%s", rec, debug.Stack())
		}
	}()
	return a.run(ctx, history, userQuery)
}

func (a *Agent) run(ctx context.Context, history []llm.Message, userQuery string) ([]llm.Message, *Result, error) {
	userQuery = strings.TrimSpace(userQuery)
	if userQuery == "" {
		return nil, nil, fmt.Errorf("user query is empty")
	}
	// Initialize todos from session state (empty for one-shot runs).
	a.todos = append([]tools.TodoItem(nil), a.opts.InitialTodos...)
	a.turnMutatingTools = 0
	a.resetExploreFirstGate()
	a.overflowRecoveries = 0
	a.contextPressureWarned = false
	a.tools.ResetDeptLessonBudget()
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
	cb.SetOnClassified(func(kind ErrorKind, meta RecordMeta) {
		if a.opts.AgentLogger == nil {
			return
		}
		detail := meta.Detail
		if detail == "" && meta.Err != nil {
			detail = meta.Err.Error()
		}
		a.opts.AgentLogger.LogStepClassified(steps, kind.String(), meta.ToolName, detail)
	})

	emitStepDone := func(reason string) {
		if a.opts.OnEvent != nil && reason != "" {
			a.opts.OnEvent(AgentEvent{Step: steps, Stream: llm.StreamEvent{
				Kind:    llm.StreamEventStepDone,
				Content: reason,
			}})
		}
	}

	// Mid-turn persistence hook (resilience audit P2): after each completed
	// tool step, hand the caller a copy of the accumulated history so a
	// crash/kill mid-turn loses at most one step of LLM work, not the whole
	// turn. Hook panics must not kill the loop.
	notifyStepHistory := func(h []llm.Message) {
		if a.opts.OnStepHistory == nil {
			return
		}
		snap := append([]llm.Message(nil), h...)
		step := steps
		safeRun("OnStepHistory", func() { a.opts.OnStepHistory(step, snap) })
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
		keep := a.opts.HistoryPruneKeepRecent
		if keep <= 0 {
			keep = defaultHistoryPruneKeepRecent
		}
		var protect []string
		if a.working != nil {
			protect = a.working.ActiveFiles()
		}
		if a.opts.ToolDigestBytes > 0 {
			history = pruneRetroactiveToolHistory(history, a.opts.ToolDigestBytes, keep, protect...)
		}
		if a.opts.Mode == ModeOrchestra {
			history = collapseOrchestraWorkerTaskHistory(history, keep)
		}

		// Soft notice once when context approaches the compact threshold
		// (warnPct ? compactPct?5, floor 50%) so TUI can hint before a full compact.
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
		// threshold â each step burning an LLM call to produce a slightly larger
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
						a.logf("compaction did not converge: %d > %d bytes (?80%% retained); falling back to truncation", before, after)
						history = truncateMessages(history, a.opts.MaxPromptBytes)
						a.recordCompactMetrics(before, historyBytes(history), false)
					} else {
						a.logf("history compacted: %d bytes > %d bytes", before, after)
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
				notifyStepHistory(history)
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
			notifyStepHistory(history)
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
	// A hard error here used to leave UI chat on disk but empty agent history 
	// reopen then forced the model to re-read everything.
	if a.opts.OnEvent != nil {
		a.opts.OnEvent(AgentEvent{Step: steps, Stream: llm.StreamEvent{
			Kind:    llm.StreamEventRecoverableError,
			Content: "MAX_STEPS - history saved, continue with a new message",
		}})
	}
	return history, &Result{
		Steps:            steps,
		Todos:            a.todos,
		MaxStepsExceeded: true,
		StopReason:       "max_steps",
	}, nil
}