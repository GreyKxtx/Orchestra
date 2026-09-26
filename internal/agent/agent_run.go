package agent

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"

	agentformat "github.com/orchestra/orchestra/internal/agent/format"
	"github.com/orchestra/orchestra/internal/agent/guard"
	agenthistory "github.com/orchestra/orchestra/internal/agent/history"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/protocol"

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

func (a *Agent) run(ctx context.Context, history []llm.Message, userQuery string) (outHistory []llm.Message, result *Result, err error) {
	a.opts.AgentLogger = a.baseLogger.For(ctx)
	a.stageCtx = tools.LayerContext(ctx)
	a.turnPrompt = nil
	userQuery = strings.TrimSpace(userQuery)
	if userQuery == "" {
		return nil, nil, fmt.Errorf("user query is empty")
	}
	a.beginTurn(ctx, userQuery)
	defer a.persistWorkingTurnDigest()
	// Named returns: recordTurnLesson runs after result is fully built by
	// whichever return statement fired, so it can still attach a
	// RuleSuggestion to it — this is the one place that turn's *Result
	// pointer is reachable before it goes back to the caller.
	defer func() { a.recordTurnLesson(result) }()

	l := a.newTurnLoop(ctx, userQuery)
	// Set the moment the loop below replaces the history array wholesale
	// instead of appending to it. Stamped onto the Result from a defer rather
	// than at each return, because this loop returns from a dozen places and a
	// path that forgot to copy it would silently hand the core stale history
	// indices — see Result.HistoryRewritten for what that costs.
	defer func() {
		if result != nil {
			result.HistoryRewritten = l.historyRewritten
		}
	}()

	if history == nil {
		history = make([]llm.Message, 0, 32)
	}
	for l.steps < a.opts.MaxSteps {
		l.steps++

		// M3 in audit ledger: honour parent ctx cancellation at the top of
		// every loop iteration so $/cancelRequest unwinds the run even
		// before the next LLM call sees it. Without this, the loop kept
		// running compaction + nextStep before noticing the cancel.
		if err := ctx.Err(); err != nil {
			return history, nil, err
		}
		history, err = l.prepareHistory(history)
		if err != nil {
			return history, nil, err
		}
		ans, shrunk, retry, err := l.modelStep(history)
		if err != nil {
			return history, nil, err
		}
		if retry {
			history = shrunk
			continue
		}

		var done bool
		switch ans.step.Type {
		case StepToolCall:
			history, result, done, err = l.toolStep(history, ans)
		case StepFinal:
			history, result, done, err = l.finalStep(history, ans)
		default:
			// M7+M8 in audit ledger: unknown step type counts toward the
			// MaxInvalidRetries cap so a model emitting persistently bogus
			// step shapes can't loop forever within MaxSteps.
			history, result, done, err = l.invalidStep(history, ans.raw, "Invalid JSON format: unknown step type")
		}
		if done || err != nil {
			return history, result, err
		}
	}
	return l.finishOnMaxSteps(history)
}

// beginTurn resets what one turn carries on the agent and gathers what the
// first step is given: the todos, the CKG context and the rules of the
// packages the query is about.
func (a *Agent) beginTurn(ctx context.Context, userQuery string) {
	// Initialize todos from session state (empty for one-shot runs).
	a.todos = append([]tools.TodoItem(nil), a.opts.InitialTodos...)
	a.turnMutatingTools = 0
	a.turnMutatedPaths = nil
	a.groundingCorrected = false
	a.codeChangeReminded = false
	a.resetExploreFirstGate()
	a.overflowRecoveries = 0
	a.finalsWhileTasksRun = 0
	a.llmInfraErr = nil
	a.contextPressureWarned = false
	a.runCtx = ctx
	a.tools.ResetDeptLessonBudget(ctx)
	a.initWorkingState(userQuery)
	// Pre-fetch relevant CKG nodes once per Run (injected only on step 1).
	a.ckgContext = a.tools.FetchCKGContext(ctx, userQuery)
	// Rules of the packages this turn is about, before the model opens a single
	// file. An @-mention or an attachment is a statement about which code the
	// turn concerns; until now those rules loaded only once a tool had read the
	// file, so they arrived a step late — or never, when the model answered
	// from the mention alone.
	//
	// Once per Run, not per step: discoverInstructions dedupes by directory for
	// the life of the runner, so recomputing it on step 2 returns nothing and
	// the rules would drop out of the prompt mid-turn.
	a.queryInstructions = a.tools.InstructionsForQuery(ctx, userQuery)
}

// turnLoop is the state of one run's loop: the step counter, the circuit
// breaker and the flags the phases below share. Each phase takes the history
// and gives it back; a phase that ends the turn says so.
type turnLoop struct {
	a         *Agent
	ctx       context.Context
	userQuery string
	steps     int
	cb        *guard.CircuitBreaker
	// historyRewritten is set the moment a phase replaces the history array
	// wholesale (compaction, truncation, overflow recovery) instead of
	// appending to it.
	historyRewritten     bool
	maxStepsReminderSent bool
}

// modelAnswer is one parsed step of the model.
type modelAnswer struct {
	step *Step
	raw  string
	resp *llm.CompleteResponse
}

func (a *Agent) newTurnLoop(ctx context.Context, userQuery string) *turnLoop {
	l := &turnLoop{a: a, ctx: ctx, userQuery: userQuery}
	a.syncModelContextFromClient()
	l.cb = guard.NewCircuitBreaker(a.opts.MaxDeniedToolRepeats, a.opts.MaxToolErrorRepeats, a.opts.MaxFinalFailures, a.opts.MaxInvalidRetries)
	// syncModelContextFromClient ran just above, so this is the window the
	// server actually reports. On a small one the repeat budget shrinks: two
	// copies of a file is a large share of a 16k prompt.
	l.cb.SetContextWindow(a.opts.ModelContextTokens)
	l.cb.ResetDedup()
	l.cb.SetOnClassified(func(kind guard.ErrorKind, meta guard.RecordMeta) {
		if a.opts.AgentLogger == nil {
			return
		}
		detail := meta.Detail
		if detail == "" && meta.Err != nil {
			detail = meta.Err.Error()
		}
		a.opts.AgentLogger.LogStepClassified(l.steps, kind.String(), meta.ToolName, detail)
	})
	return l
}

func (l *turnLoop) emitStepDone(reason string) {
	if l.a.opts.OnEvent != nil && reason != "" {
		l.a.opts.OnEvent(AgentEvent{Step: l.steps, Stream: llm.StreamEvent{
			Kind:    llm.StreamEventStepDone,
			Content: reason,
		}})
	}
}

// notifyStepHistory is the mid-turn persistence hook (resilience audit
// P2): after each completed tool step, hand the caller a copy of the
// accumulated history so a crash/kill mid-turn loses at most one step of
// LLM work, not the whole turn. Hook panics must not kill the loop.
//
// The rewrite flag is read here, inside the one method both call sites go
// through, rather than passed in by them: a call site that had to remember
// to forward it is a call site that can forget, and forgetting means the
// caller persists a rewritten array under indices recorded for the old one.
func (l *turnLoop) notifyStepHistory(h []llm.Message) {
	if l.a.opts.OnStepHistory == nil {
		return
	}
	snap := append([]llm.Message(nil), h...)
	step := l.steps
	rewritten := l.historyRewritten
	agentformat.SafeRun("OnStepHistory", func() { l.a.opts.OnStepHistory(step, snap, rewritten) })
}

// recoverable emits a recoverable-error event to the client.
func (l *turnLoop) recoverable(content string) {
	if l.a.opts.OnEvent != nil {
		l.a.opts.OnEvent(AgentEvent{Step: l.steps, Stream: llm.StreamEvent{
			Kind:    llm.StreamEventRecoverableError,
			Content: content,
		}})
	}
}

// prepareHistory is what happens to the history before a step: older tool
// outputs are pruned, the client is warned once when the context nears the
// compact threshold, the history is compacted past it, the notes other
// agents left arrive, and the step-limit reminder is injected at 2/3 of
// MaxSteps. It fires at the top of the loop so the history is always in a
// consistent state (no orphaned tool_calls without tool_results).
func (l *turnLoop) prepareHistory(history []llm.Message) ([]llm.Message, error) {
	a := l.a
	// Retroactive prune: shrink older tool outputs already in history.
	keep := a.opts.HistoryPruneKeepRecent
	if keep <= 0 {
		keep = agenthistory.DefaultHistoryPruneKeepRecent
	}
	var protect []string
	if a.working != nil {
		protect = a.working.ActiveFiles()
	}
	if a.opts.ToolDigestBytes > 0 {
		history = agenthistory.PruneRetroactiveToolHistory(history, a.opts.ToolDigestBytes, keep, protect...)
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
			l.recoverable("CONTEXT_PRESSURE")
		}
	}

	var err error
	if history, err = l.compact(history); err != nil {
		return history, err
	}

	// Notes other agents left for this one since its last step (agency
	// agent_post). Here, before the step, the history holds no half
	// finished tool exchange for the note to split.
	history = a.drainAgencyInbox(history)

	// Inject a step-limit warning once at 2/3 of MaxSteps. As a user
	// message: an assistant one here could end the request, which recent
	// Claude models reject, and read to the model as its own words.
	if !l.maxStepsReminderSent && l.steps*3 >= a.opts.MaxSteps*2 {
		l.maxStepsReminderSent = true
		history = append(history, llm.Message{
			Role:    llm.RoleUser,
			Content: a.maxStepsReminder(),
		})
	}

	// Refresh TUI ctx bar every step. Local providers (LM Studio) often omit
	// stream usage; the estimate keeps prompt_ctx / status bar non-zero.
	// Real usage from Done/emitStepUsage overwrites when the provider reports it.
	a.emitPromptContextEstimate(l.steps, history)
	return history, nil
}

// compact summarises the history before the next LLM call once it is
// large — or once, on demand.
//
// H15 in audit ledger: convergence guard. The previous loop kept calling
// compactHistory every step when the summary itself still exceeded the
// threshold -- each step burning an LLM call to produce a slightly larger
// summary-of-summary until MaxSteps tripped. We now (a) refuse to use a
// "compacted" result that didn't actually shrink history by at least 20%,
// and (b) fall back to plain truncateMessages when compaction declines
// to converge, breaking the loop on the very next step.
func (l *turnLoop) compact(history []llm.Message) ([]llm.Message, error) {
	a := l.a
	if a.opts.CompactThresholdPct <= 0 || a.opts.MaxPromptBytes <= 0 {
		return history, nil
	}
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
	if !need || historyBytes(history) == 0 {
		return history, nil
	}
	if a.llmInfraErr != nil {
		return history, a.llmInfraErr
	}
	before := historyBytes(history)
	compacted, compactErr := a.compactHistory(l.ctx, l.userQuery, history)
	switch {
	case compactErr != nil:
		if llm.IsUnreachableError(compactErr) {
			return history, compactErr
		}
		a.logf("compaction failed (non-fatal), continuing with truncation: %v", compactErr)
		history = agenthistory.TruncateMessages(history, a.opts.MaxPromptBytes)
		l.historyRewritten = true
	case historyBytes(compacted)*5 >= before*4: // < 20% shrink
		a.logf("compaction did not converge: %d > %d bytes (>=80%% retained); falling back to truncation", before, historyBytes(compacted))
		history = agenthistory.TruncateMessages(history, a.opts.MaxPromptBytes)
		// Truncation is not compaction, but it drops entries from the
		// front just the same: every index into the old array is now wrong.
		l.historyRewritten = true
	default:
		a.logf("history compacted: %d bytes > %d bytes", before, historyBytes(compacted))
		history = compacted
		l.historyRewritten = true
		// Forgive one repeat, do not forget them all: on a small window
		// compaction runs every few steps, and clearing the counters reset
		// the doom-loop guard faster than it could trip.
		l.cb.ForgiveReadOnlyCallsAfterCompaction()
		l.recoverable("CONTEXT_COMPACTED")
	}
	if afterBytes := historyBytes(history); afterBytes < before {
		a.emitPromptContextEstimate(l.steps, history)
	}
	return history, nil
}

// modelStep asks the model for the next step. A request the provider
// rejected as too large is recovered from by compacting and replaying the
// step (OpenCode-style): retry is then true with the shrunk history, and the
// step does not count.
func (l *turnLoop) modelStep(history []llm.Message) (ans *modelAnswer, shrunk []llm.Message, retry bool, err error) {
	a := l.a
	step, raw, llmResp, err := a.nextStep(l.ctx, l.userQuery, history, l.steps)
	if err != nil {
		if llm.IsUnreachableError(err) {
			a.llmInfraErr = err
			return nil, nil, false, err
		}
		if l.ctx.Err() == nil && llm.IsContextOverflowError(err) {
			if historyBytes(history) == 0 {
				// nextStep raises this same hint when it refuses a first
				// step it can already measure as too large, and wrapping
				// it again printed the sentence twice in one line.
				if strings.Contains(err.Error(), contextWindowHint) {
					return nil, nil, false, err
				}
				return nil, nil, false, fmt.Errorf("%w — %s", err, contextWindowHint)
			}
			if shrunk, ok := a.recoverFromOverflow(l.ctx, l.userQuery, history, err, l.steps); ok {
				// ok == true only when recovery actually reclaimed bytes,
				// which it does by compacting and/or truncating the array
				// — another wholesale rewrite, not an append.
				l.historyRewritten = true
				l.steps--
				return nil, shrunk, true, nil
			}
			if a.llmInfraErr != nil {
				return nil, nil, false, a.llmInfraErr
			}
		}
		return nil, nil, false, err
	}
	if step == nil {
		return nil, nil, false, fmt.Errorf("nextStep returned nil step without error")
	}
	return &modelAnswer{step: step, raw: raw, resp: llmResp}, nil, false, nil
}

// invalidStep answers a step the loop cannot use with a validator error,
// counting it toward the invalid-retries cap.
func (l *turnLoop) invalidStep(history []llm.Message, raw, msg string) ([]llm.Message, *Result, bool, error) {
	if cbErr := l.cb.RecordInvalid(); cbErr != nil {
		h, r, err := l.a.stopOnBreaker(history, l.steps, cbErr)
		return h, r, true, err
	}
	history = append(history, llm.Message{
		Role:    llm.RoleUser,
		Content: agentformat.ValidatorError(msg, raw),
	})
	l.emitStepDone("invalid")
	return history, nil, false, nil
}

// toolStep runs the calls of a tool_call step: a parallel batch when every
// call is parallel-safe, else one at a time. done is true when the turn ends
// here — a breaker tripped, or a call produced the turn's result.
func (l *turnLoop) toolStep(history []llm.Message, ans *modelAnswer) ([]llm.Message, *Result, bool, error) {
	a := l.a
	calls := a.resolveToolCalls(ans.step, ans.resp)
	if len(calls) == 0 {
		return l.invalidStep(history, ans.raw, "Invalid JSON format: tool is required")
	}

	toolDefs := a.buildToolDefs()
	if len(calls) >= 2 && allParallelSafeCalls(calls, toolDefs) {
		var cbErr *protocol.Error
		history, cbErr = a.runParallelToolBatch(l.ctx, l.cb, history, calls, ans.resp, l.steps)
		if cbErr != nil {
			h, r, err := a.stopOnBreaker(history, l.steps, cbErr)
			return h, r, true, err
		}
		l.emitStepDone("tool_call")
		l.notifyStepHistory(history)
		a.maybePersistMicroDigest(l.steps)
		return history, nil, false, nil
	}

	hasToolCalls := ans.resp != nil && len(ans.resp.Message.ToolCalls) > 0
	if hasToolCalls {
		history = append(history, llm.Message{
			Role:      llm.RoleAssistant,
			Content:   "",
			ToolCalls: ans.resp.Message.ToolCalls,
			// Anthropic wants the thinking back beside the tool_use
			// it led to, or the next step is a 400 (LLM-6).
			Thinking: ans.resp.Message.Thinking,
		})
		a.logf("agent.tool_call added assistant message to history, history_len=%d, tool_calls=%d", len(history), len(calls))
	} else {
		a.logf("agent.tool_call WARNING: no tool_calls in response, history_len=%d", len(history))
	}

	// Hints a call appends (LSP errors, "staged ready", repeat
	// warnings, screenshots) wait until every call of the batch has
	// its reply: a user message between two replies splits the batch
	// and the provider rejects the second reply as an orphan.
	var deferred []llm.Message
	for _, tc := range calls {
		mark := len(history)
		outcome, err := a.runSerialToolCall(l.ctx, l.cb, &history, tc, l.steps, l.emitStepDone)
		history, deferred = deferNonToolMessages(history, mark, deferred)
		if err != nil {
			h, r, err := a.stopOnBreaker(append(history, deferred...), l.steps, err)
			return h, r, true, err
		}
		if outcome.EarlyResult != nil {
			return append(history, deferred...), outcome.EarlyResult, true, nil
		}
	}
	history = append(history, deferred...)
	l.emitStepDone("tool_call")
	l.notifyStepHistory(history)
	a.maybePersistMicroDigest(l.steps)
	return history, nil, false, nil
}

// finalStep handles a final step: it waits for the tasks still running,
// refuses a final that comes too early, then commits the staged edits.
// done is true when the turn ends here.
func (l *turnLoop) finalStep(history []llm.Message, ans *modelAnswer) ([]llm.Message, *Result, bool, error) {
	a := l.a
	if msg, wait := a.finalWithRunningTasks(l.ctx); wait {
		history = append(history, msg)
		l.emitStepDone("invalid")
		return history, nil, false, nil
	}
	if hint, reject := a.rejectPrematureFinal(l.userQuery, ans.step, ans.raw, l.steps); reject {
		if cbErr := l.cb.RecordInvalid(); cbErr != nil {
			h, r, err := a.stopOnBreaker(history, l.steps, cbErr)
			return h, r, true, err
		}
		history = append(history, llm.Message{
			Role:    llm.RoleUser,
			Content: agentformat.ValidatorErrorCompact(hint),
		})
		// Empty-response retries stay in the agent loop; skip TUI spam.
		if !isSilentPrematureFinalHint(hint) {
			l.recoverable(hint)
		}
		l.emitStepDone("invalid")
		return history, nil, false, nil
	}
	outcome, err := a.handleFinalStep(l.ctx, l.cb, &history, ans.step, ans.resp, l.steps, ans.raw, l.emitStepDone)
	if err != nil {
		return history, nil, true, err
	}
	if outcome.Retry {
		return history, nil, false, nil
	}
	return history, outcome.Result, true, nil
}

// finishOnMaxSteps ends a turn that ran out of steps: with the staged
// result when one can be flushed, else as a soft stop that keeps the
// history so SessionMessage can persist it. A hard error here used to leave
// UI chat on disk but empty agent history — reopen then forced the model to
// re-read everything.
func (l *turnLoop) finishOnMaxSteps(history []llm.Message) ([]llm.Message, *Result, error) {
	a := l.a
	if res, ok := a.finalizeOnMaxSteps(l.ctx, history, l.steps); ok {
		return history, res, nil
	}
	l.recoverable("MAX_STEPS - history saved, continue with a new message")
	return history, &Result{
		Steps:            l.steps,
		Todos:            a.todos,
		MaxStepsExceeded: true,
		StopReason:       "max_steps",
	}, nil
}

// deferNonToolMessages moves the messages a serial tool call appended after
// history[mark:] that are not tool replies into deferred, keeping the replies
// in place and in order.
func deferNonToolMessages(history []llm.Message, mark int, deferred []llm.Message) ([]llm.Message, []llm.Message) {
	if mark >= len(history) {
		return history, deferred
	}
	kept := history[:mark]
	for _, m := range history[mark:] {
		if m.Role == llm.RoleTool {
			kept = append(kept, m)
			continue
		}
		deferred = append(deferred, m)
	}
	return kept, deferred
}
