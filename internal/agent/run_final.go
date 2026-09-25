package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	agentformat "github.com/orchestra/orchestra/internal/agent/format"
	"github.com/orchestra/orchestra/internal/agent/guard"

	"github.com/orchestra/orchestra/internal/plan"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/patch/applier"
	"github.com/orchestra/orchestra/patch/ops"
	"github.com/orchestra/orchestra/patch/patches"
	"github.com/orchestra/orchestra/protocol"
)

// staging is the context the run stages under: its task layer, if it has one.
func (a *Agent) staging() context.Context {
	if a.stageCtx == nil {
		return context.Background()
	}
	return a.stageCtx
}

// finalizeOnMaxSteps flushes staged changes when the step budget is exhausted
// so dry-run/preview runs do not lose write/edit progress made via tools.
func (a *Agent) finalizeOnMaxSteps(ctx context.Context, history []llm.Message, steps int) (*Result, bool) {
	stagedOps := a.tools.StagedOps(a.staging())
	if len(stagedOps) == 0 {
		return nil, false
	}
	a.logf("max_steps: flushing %d staged op(s) before exit", len(stagedOps))
	resp, err := a.tools.FSApplyOps(ctx, tools.FSApplyOpsRequest{
		Ops:    stagedOps,
		DryRun: !a.opts.Apply,
		Backup: a.opts.Backup && a.opts.Apply,
	})
	if err != nil {
		a.logf("max_steps staged flush failed: %v", err)
		return nil, false
	}
	return &Result{
		Steps:            steps,
		Ops:              stagedOps,
		Applied:          a.opts.Apply,
		ApplyResponse:    resp,
		Todos:            a.todos,
		MaxStepsExceeded: true,
		StopReason:       "max_steps",
	}, true
}

// finalStepOutcome is the control-flow result of handleFinalStep.
// Retry=true means the loop should continue (recoverable resolve/apply failure).
// Result non-nil means the agent run should return successfully.
type finalStepOutcome struct {
	Retry  bool
	Result *Result
}

// handleFinalStep commits staged changes from edit/write (and optional final.patches).
// Tools always run through staging during the turn; Apply=true writes to disk here.
func (a *Agent) handleFinalStep(
	ctx context.Context,
	cb *guard.CircuitBreaker,
	history *[]llm.Message,
	step *Step,
	llmResp *llm.CompleteResponse,
	steps int,
	raw string,
	emitStepDone func(string),
) (finalStepOutcome, error) {
	if step.Final == nil {
		*history = append(*history, llm.Message{
			Role:    llm.RoleUser,
			Content: agentformat.ValidatorError("Invalid JSON format: final is required", raw),
		})
		emitStepDone("invalid")
		return finalStepOutcome{Retry: true}, nil
	}

	finalPatches := append([]patches.Patch{}, step.Final.Patches...)

	// Before anything reads a patch's type, make the type agree with the
	// fields. Every guard below switches on Type, so a mislabelled patch slips
	// past the ones meant for what it actually is.
	finalPatches, normalized := normalizeFinalPatches(finalPatches)
	for _, note := range normalized {
		a.logf("final patch relabelled %s", note)
		if a.opts.AgentLogger != nil {
			a.opts.AgentLogger.LogStepClassified(steps, "patch_relabelled", "", note)
		}
	}

	if a.opts.Mode == ModeOrchestra && len(finalPatches) > 0 {
		var blocked []string
		planPath := a.effectivePlanPath()
		for _, p := range finalPatches {
			if !plan.IsWritablePath(p.Path, planPath) {
				blocked = append(blocked, p.Path)
			}
		}
		if len(blocked) > 0 {
			msg := fmt.Sprintf(
				"orchestra lead cannot apply final.patches to production files (%s). Delegate code changes via task(subagent_type=worker, tier=focused, prompt=<WorkOrder JSON>).",
				strings.Join(blocked, ", "),
			)
			*history = append(*history, llm.Message{Role: llm.RoleUser, Content: msg})
			emitStepDone("invalid")
			// Refused finals count against MaxFinalFailures like failed
			// ones: a lead that keeps patching production code would
			// otherwise loop until MaxSteps (LLM-9).
			if cbErr := cb.RecordResolveFailure(errors.New(msg)); cbErr != nil {
				return finalStepOutcome{}, cbErr
			}
			return finalStepOutcome{Retry: true}, nil
		}
	}

	for _, p := range finalPatches {
		if err := a.finalPatchRefusal(p.Path); err != nil {
			a.logf("final rejected: patch for %s: %v", p.Path, err)
			*history = append(*history, llm.Message{Role: llm.RoleUser, Content: "final.patches refused: " + err.Error()})
			emitStepDone("invalid")
			if cbErr := cb.RecordDenied("final.patches"); cbErr != nil {
				return finalStepOutcome{}, cbErr
			}
			return finalStepOutcome{Retry: true}, nil
		}
	}

	if hint := a.restatedPatchHint(finalPatches); hint != "" {
		a.logf("final rejected: a patch restates a change already staged this turn")
		*history = append(*history, llm.Message{Role: llm.RoleUser, Content: hint})
		emitStepDone("invalid")
		if cbErr := cb.RecordResolveFailure(errors.New("final restates a change already staged")); cbErr != nil {
			return finalStepOutcome{}, cbErr
		}
		return finalStepOutcome{Retry: true}, nil
	}

	if len(finalPatches) > 0 {
		a.logf("final received patches=%d -> applying to staging overlay", len(finalPatches))
		start := time.Now()
		if err := a.tools.ApplyPatchesToStaged(a.staging(), finalPatches); err != nil {
			resolveMS := time.Since(start).Milliseconds()
			a.logf("staged-apply status=error duration_ms=%d err=%v", resolveMS, err)
			*history = append(*history, llm.Message{
				Role:    llm.RoleUser,
				Content: agentformat.ResolveErrorCompact(err),
			})
			if cbErr := cb.RecordResolveFailure(err); cbErr != nil {
				return finalStepOutcome{}, cbErr
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
			return finalStepOutcome{Retry: true}, nil
		}
	}

	stagedOps := a.tools.StagedOps(a.staging())
	if len(stagedOps) == 0 {
		a.logf("final: no staged ops (no changes needed)")
		if llmResp != nil {
			*history = append(*history, llmResp.Message)
		}
		emitStepDone("final")
		return finalStepOutcome{Result: &Result{
			Steps:      steps,
			Patches:    finalPatches,
			Applied:    false,
			Todos:      a.todos,
			StopReason: a.computeStopReason(false),
		}}, nil
	}

	a.logf("final staged_ops=%d -> FSApplyOps dry_run=%v", len(stagedOps), !a.opts.Apply)
	// What the final apply is about to write, per file. The tools already
	// committed their own changes to disk by now, so anything still staged
	// here overwrites what is there — and an op carrying empty content
	// silently empties a file the turn had already written correctly.
	if a.opts.AgentLogger != nil {
		for _, op := range stagedOps {
			n := 0
			if op.WriteAtomic != nil {
				n = len(op.WriteAtomic.Content)
			}
			a.opts.AgentLogger.LogDiskCommit("final:"+op.Path, n, "")
		}
	}

	start := time.Now()
	resp, err := a.tools.FSApplyOps(ctx, tools.FSApplyOpsRequest{
		Ops:    stagedOps,
		DryRun: !a.opts.Apply,
		Backup: a.opts.Backup && a.opts.Apply,
	})
	applyMS := time.Since(start).Milliseconds()
	if err != nil {
		if pe, ok := protocol.AsError(err); ok && (pe.Code == protocol.StaleContent || pe.Code == protocol.AmbiguousMatch) {
			a.logf("staged-fsapply status=recoverable_error duration_ms=%d err=%v", applyMS, err)
			*history = append(*history, llm.Message{
				Role:    llm.RoleUser,
				Content: agentformat.ApplyErrorCompact(err, pe.Code),
			})
			if cbErr := cb.RecordApplyRecoverable(err); cbErr != nil {
				return finalStepOutcome{}, cbErr
			}
			if a.opts.OnEvent != nil {
				a.opts.OnEvent(AgentEvent{Step: steps, Stream: llm.StreamEvent{
					Kind:    llm.StreamEventRecoverableError,
					Content: fmt.Sprintf("%s: %s", pe.Code, pe.Message),
				}})
			}
			emitStepDone("final_retry")
			return finalStepOutcome{Retry: true}, nil
		}
		a.logf("staged-fsapply status=error duration_ms=%d err=%v", applyMS, err)
		return finalStepOutcome{}, err
	}
	a.logf("staged-apply status=ok duration_ms=%d diffs=%d applied=%v", applyMS, len(resp.Diffs), a.opts.Apply)

	if llmResp != nil {
		*history = append(*history, llmResp.Message)
	}
	if a.opts.OnEvent != nil && resp != nil {
		a.emitPendingOpsEvent(steps, stagedOps, resp.Diffs, a.opts.Apply)
	}
	emitStepDone("final")
	return finalStepOutcome{Result: &Result{
		Steps:         steps,
		Patches:       finalPatches,
		Ops:           stagedOps,
		Applied:       a.opts.Apply,
		ApplyResponse: resp,
		Todos:         a.todos,
		StopReason:    a.computeStopReason(false),
	}}, nil
}

func (a *Agent) computeStopReason(maxSteps bool) string {
	if maxSteps {
		return "max_steps"
	}
	if countOpenTodos(a.todos) > 0 {
		return "partial"
	}
	return "completed"
}

// commitStagedAfterMutatingTool flushes one staged write/edit to disk immediately
// when Apply=true so files exist before the turn ends (TUI live apply).
func (a *Agent) commitStagedAfterMutatingTool(ctx context.Context, steps int, toolPath string) {
	toolPath = strings.TrimSpace(toolPath)
	if !a.opts.Apply || toolPath == "" {
		return
	}
	resp, err := a.tools.CommitStagedPath(ctx, toolPath, a.opts.Backup)
	if err != nil {
		if a.opts.AgentLogger != nil {
			a.opts.AgentLogger.LogDiskCommit(toolPath, 0, err.Error())
		}
		a.logf("incremental commit path=%s err=%v", toolPath, err)
		if a.opts.OnEvent != nil {
			a.opts.OnEvent(AgentEvent{Step: steps, Stream: llm.StreamEvent{
				Kind:    llm.StreamEventRecoverableError,
				Content: "commit " + toolPath + ": " + err.Error(),
			}})
		}
		return
	}
	if resp == nil || len(resp.ChangedFiles) == 0 {
		// Not an error, and not nothing: the tool reported a change and the
		// commit moved no file. Worth a line, because a silent return here is
		// indistinguishable from a commit that worked.
		if a.opts.AgentLogger != nil {
			a.opts.AgentLogger.LogDiskCommit(toolPath, 0, "commit changed no file")
		}
		return
	}
	if a.opts.AgentLogger != nil {
		for _, d := range resp.Diffs {
			a.opts.AgentLogger.LogDiskCommit(d.Path, len(d.After), "")
		}
	}
	a.logf("incremental commit path=%s files=%d", toolPath, len(resp.ChangedFiles))
	a.emitPendingOpsEvent(steps, a.tools.StagedOps(a.staging()), resp.Diffs, true)
}

// previewStagedAfterMutatingTool emits pending_ops (dry-run) after each successful
// write/edit when Apply=false so VS Code/webview can show inline diff and the
// apply bar without waiting for the final step.
func (a *Agent) previewStagedAfterMutatingTool(ctx context.Context, steps int) {
	if a.opts.Apply || a.opts.OnEvent == nil {
		return
	}
	stagedOps := a.tools.StagedOps(a.staging())
	if len(stagedOps) == 0 {
		return
	}
	resp, err := a.tools.FSApplyOps(ctx, tools.FSApplyOpsRequest{
		Ops:    stagedOps,
		DryRun: true,
	})
	if err != nil {
		a.logf("staged preview err=%v", err)
		return
	}
	if resp == nil || len(resp.Diffs) == 0 {
		return
	}
	a.logf("staged preview ops=%d diffs=%d step=%d", len(stagedOps), len(resp.Diffs), steps)
	a.emitPendingOpsEvent(steps, stagedOps, resp.Diffs, false)
}

func (a *Agent) emitPendingOpsEvent(steps int, stagedOps []ops.AnyOp, diffs []applier.FileDiff, applied bool) {
	if a.opts.OnEvent == nil {
		return
	}
	payload := map[string]any{
		"ops":     stagedOps,
		"diff":    diffs,
		"applied": applied,
	}
	if len(stagedOps) == 0 {
		payload["ops"] = []any{}
	}
	payloadJSON, _ := json.Marshal(payload)
	a.opts.OnEvent(AgentEvent{Step: steps, Stream: llm.StreamEvent{
		Kind:    llm.StreamEventPendingOps,
		Content: string(payloadJSON),
	}})
}

func (a *Agent) maybeHintStagedReady(history *[]llm.Message, toolPath string) {
	toolPath = strings.TrimSpace(toolPath)
	if toolPath == "" || a.opts.Apply {
		return
	}
	if a.turnMutatingTools < 1 {
		return
	}
	if a.tools == nil || len(a.tools.StagedOps(a.staging())) == 0 {
		return
	}
	*history = append(*history, llm.Message{
		Role: llm.RoleUser,
		Content: fmt.Sprintf(
			"Staged changes for %s are ready for the user to review/apply. If the task is complete, respond with final {\"patches\":[]} — avoid another read/edit/write on the same file unless the last change failed.",
			toolPath,
		),
	})
}

// stopOnBreaker decides what a turn stopped by the circuit breaker returns.
//
// Running out of steps already returns a Result rather than an error — the
// edits are on disk and pretending otherwise helps nobody. A breaker trip is
// the same situation: the model went in circles, but the work it did before
// that is real and committed. Returning only the error threw it away, and a
// caller could not tell "nothing happened" from "everything happened and then
// the model would not stop talking".
//
// A turn that changed nothing has nothing to report, so it still fails: the
// breaker has to stay a signal.
func (a *Agent) stopOnBreaker(history []llm.Message, steps int, cbErr error) ([]llm.Message, *Result, error) {
	if a.turnMutatingTools == 0 {
		return history, nil, cbErr
	}
	reason := "blocked"
	a.logf("breaker stopped the turn after %d mutating tool call(s); reporting the work: %v",
		a.turnMutatingTools, cbErr)
	if a.opts.OnEvent != nil {
		a.opts.OnEvent(AgentEvent{Step: steps, Stream: llm.StreamEvent{
			Kind: llm.StreamEventRecoverableError,
			Content: "the turn was stopped (" + cbErr.Error() +
				"), but the changes it already made are kept",
		}})
	}
	return history, &Result{
		Steps:      steps,
		Applied:    a.opts.Apply,
		Todos:      a.todos,
		StopReason: reason,
	}, nil
}
