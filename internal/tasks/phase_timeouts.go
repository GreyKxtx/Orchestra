package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/decisions"
	"github.com/orchestra/orchestra/internal/orchestrastate"
	"github.com/orchestra/orchestra/internal/tools"
)

// Phase timeouts (spec §4.5, checklist 28) — deadlock breakers on top of the
// phase machine:
//
//   - discovery_s / contract_s: a phase that outlives its budget attaches a
//     phase_timeout advisory to every task_result so the orchestrator
//     escalates to the user instead of silently spinning;
//   - lead_brief_s: wall-clock cap on architecture (Lead) children when the
//     spawn does not set its own timeout;
//   - blocked_escalate_s: a blockage that persists longer than the threshold
//     forces a runtime question to the user — zero orchestrator turns.

// leadBriefTimeoutMS returns the effective timeout for an architecture child:
// the explicit request wins; otherwise lead_brief_s applies in orchestrated
// sessions (state.md exists). 0 = no cap.
func (r *TaskRunner) leadBriefTimeoutMS(subagentType string, requestedMS int) int {
	if requestedMS > 0 {
		return requestedMS
	}
	capS := r.child.PhaseTimeouts.LeadBriefS
	if capS <= 0 || !strings.EqualFold(strings.TrimSpace(subagentType), "architecture") {
		return requestedMS
	}
	if _, found, err := orchestrastate.Load(r.toolRunner.WorkspaceRoot()); err != nil || !found {
		return requestedMS
	}
	return capS * 1000
}

// annotatePhaseTimeout attaches a phase_timeout advisory to the task_result
// when the current phase has outlived its budget (JSON results only — the
// orchestrator parses the field; free-text results pass through).
func (r *TaskRunner) annotatePhaseTimeout(taskResult string) string {
	t := r.child.PhaseTimeouts
	if t.DiscoveryS <= 0 && t.ContractS <= 0 {
		return taskResult
	}
	st, found, err := orchestrastate.Load(r.toolRunner.WorkspaceRoot())
	if err != nil || !found {
		return taskResult
	}
	warn := st.PhaseTimeoutWarning(t, time.Now().UTC())
	if warn == "" {
		return taskResult
	}
	return attachBarrierPayload(taskResult, map[string]any{"phase_timeout": warn})
}

// trackBlockedEscalation maintains blocked_since in state.md and forces a
// user question once a blockage persists past blocked_escalate_s. The answer
// goes verbatim to decisions.md and into the task_result (escalation_answer).
func (r *TaskRunner) trackBlockedEscalation(ctx context.Context, taskResult string) string {
	root := r.toolRunner.WorkspaceRoot()
	st, found, err := orchestrastate.Load(root)
	if err != nil || !found {
		return taskResult
	}
	if !taskResultIsBlocked(taskResult) {
		if st.BlockedSince != "" {
			st.BlockedSince = ""
			_ = orchestrastate.Save(root, st)
		}
		return taskResult
	}

	now := time.Now().UTC()
	if st.BlockedSince == "" {
		st.BlockedSince = now.Format(time.RFC3339)
		_ = orchestrastate.Save(root, st)
		return taskResult
	}
	threshold := r.child.PhaseTimeouts.BlockedEscalateS
	since, perr := time.Parse(time.RFC3339, st.BlockedSince)
	if threshold <= 0 || perr != nil || int(now.Sub(since).Seconds()) <= threshold {
		return taskResult
	}
	if r.child.QuestionAsker == nil || r.child.RelayViaLLM {
		// No interactive channel — surface the fact so the orchestrator
		// escalates on its own.
		return attachBarrierPayload(taskResult, map[string]any{
			"blocked_escalation": "blocked longer than blocked_escalate_s and no interactive channel; escalate to the user in your next reply",
		})
	}
	reason := blockedReasonOf(taskResult)
	q := fmt.Sprintf("Pipeline blocked for %ds (reason: %s). How should we proceed?",
		int(now.Sub(since).Seconds()), reason)
	answers, aerr := r.child.QuestionAsker.Ask(ctx, []tools.QuestionItem{{
		Question: q,
		Options:  []string{"keep waiting", "switch to phase: maintenance", "grant a waiver", "replan the task"},
	}})
	if aerr != nil || len(answers) == 0 {
		return taskResult
	}
	_ = decisions.Append(root, []decisions.Entry{{
		Kind:     "decision",
		Question: "blocked_escalation: " + reason,
		Answer:   answers[0],
	}})
	st.BlockedSince = "" // window handled; restart on the next blockage
	_ = orchestrastate.Save(root, st)
	return attachBarrierPayload(taskResult, map[string]any{
		"escalation_answer": answers[0],
		"decisions_ref":     decisions.FileRel,
	})
}

func taskResultIsBlocked(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "{") || !json.Valid([]byte(raw)) {
		return false
	}
	var m struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(m.Status), "blocked")
}

func blockedReasonOf(raw string) string {
	var m struct {
		BlockedReason string `json:"blocked_reason"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &m); err == nil {
		if br := strings.TrimSpace(m.BlockedReason); br != "" {
			return br
		}
	}
	return "unspecified"
}
