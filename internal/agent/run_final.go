package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/patch/applier"
	"github.com/orchestra/orchestra/patch/ops"
	"github.com/orchestra/orchestra/patch/patches"
	"github.com/orchestra/orchestra/internal/plan"
	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/internal/tools"
)

// finalizeOnMaxSteps flushes staged changes when the step budget is exhausted
// so dry-run/preview runs do not lose write/edit progress made via tools.
func (a *Agent) finalizeOnMaxSteps(ctx context.Context, history []llm.Message, steps int) (*Result, bool) {
	stagedOps := a.tools.StagedOps()
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
	cb *CircuitBreaker,
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
			Content: formatValidatorError("Invalid JSON format: final is required", raw),
		})
		emitStepDone("invalid")
		return finalStepOutcome{Retry: true}, nil
	}

	finalPatches := append([]patches.Patch{}, step.Final.Patches...)

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
			return finalStepOutcome{Retry: true}, nil
		}
	}

	if len(finalPatches) > 0 {
		a.logf("final received patches=%d -> applying to staging overlay", len(finalPatches))
		start := time.Now()
		if err := a.tools.ApplyPatchesToStaged(finalPatches); err != nil {
			resolveMS := time.Since(start).Milliseconds()
			a.logf("staged-apply status=error duration_ms=%d err=%v", resolveMS, err)
			*history = append(*history, llm.Message{
				Role:    llm.RoleUser,
				Content: formatResolveErrorCompact(err),
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

	stagedOps := a.tools.StagedOps()
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
				Content: formatApplyErrorCompact(err, pe.Code),
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
		return
	}
	a.logf("incremental commit path=%s files=%d", toolPath, len(resp.ChangedFiles))
	a.emitPendingOpsEvent(steps, a.tools.StagedOps(), resp.Diffs, true)
}

// previewStagedAfterMutatingTool emits pending_ops (dry-run) after each successful
// write/edit when Apply=false so VS Code/webview can show inline diff and the
// apply bar without waiting for the final step.
func (a *Agent) previewStagedAfterMutatingTool(ctx context.Context, steps int) {
	if a.opts.Apply || a.opts.OnEvent == nil {
		return
	}
	stagedOps := a.tools.StagedOps()
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
	if a.tools == nil || len(a.tools.StagedOps()) == 0 {
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
