package tasks

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/orchestrastate"
)

func writeTimedState(t *testing.T, root string, phase string, phaseSince, blockedSince time.Time) {
	t.Helper()
	content := "---\norchestra:\n  phase: " + phase + "\n  prd_status: approved\n"
	if !phaseSince.IsZero() {
		content += "  phase_since: \"" + phaseSince.UTC().Format(time.RFC3339) + "\"\n"
	}
	if !blockedSince.IsZero() {
		content += "  blocked_since: \"" + blockedSince.UTC().Format(time.RFC3339) + "\"\n"
	}
	content += "---\n"
	writeFileT(t, root, ".orchestra/state.md", content)
}

func TestLeadBriefTimeoutMS(t *testing.T) {
	root := t.TempDir()
	cfg := ChildAgentConfig{PhaseTimeouts: orchestrastate.PhaseTimeouts{LeadBriefS: 600}}
	r := barrierRunner(t, root, cfg)

	// No state file → not orchestrated → no cap.
	if got := r.leadBriefTimeoutMS("architecture", 0); got != 0 {
		t.Fatalf("plain session must not be capped, got %d", got)
	}

	writeTimedState(t, root, "execution", time.Time{}, time.Time{})
	if got := r.leadBriefTimeoutMS("architecture", 0); got != 600_000 {
		t.Fatalf("architecture default cap = %d, want 600000", got)
	}
	// Explicit request wins; other subagents unaffected.
	if got := r.leadBriefTimeoutMS("architecture", 30_000); got != 30_000 {
		t.Fatalf("explicit timeout must win, got %d", got)
	}
	if got := r.leadBriefTimeoutMS("worker", 0); got != 0 {
		t.Fatalf("worker must not get the Lead cap, got %d", got)
	}
}

func TestAnnotatePhaseTimeout(t *testing.T) {
	root := t.TempDir()
	cfg := ChildAgentConfig{PhaseTimeouts: orchestrastate.PhaseTimeouts{DiscoveryS: 900, ContractS: 900}}
	r := barrierRunner(t, root, cfg)

	// Overdue contract phase → advisory attached to JSON results.
	writeTimedState(t, root, "contract", time.Now().Add(-30*time.Minute), time.Time{})
	out := r.annotatePhaseTimeout(`{"status":"done"}`)
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("result must stay JSON: %v", err)
	}
	warn, _ := m["phase_timeout"].(string)
	if !strings.Contains(warn, "contract") {
		t.Fatalf("phase_timeout advisory expected, got %q", warn)
	}

	// Fresh phase → untouched.
	writeTimedState(t, root, "contract", time.Now(), time.Time{})
	if out := r.annotatePhaseTimeout(`{"status":"done"}`); out != `{"status":"done"}` {
		t.Fatalf("fresh phase must not annotate: %s", out)
	}

	// Free-text results pass through even when overdue.
	writeTimedState(t, root, "contract", time.Now().Add(-30*time.Minute), time.Time{})
	if out := r.annotatePhaseTimeout("plain text"); out != "plain text" {
		t.Fatalf("free text must pass through: %s", out)
	}
}

func TestTrackBlockedEscalation(t *testing.T) {
	root := t.TempDir()
	asker := &scriptedAsker{answers: []string{"switch to phase: maintenance"}}
	cfg := ChildAgentConfig{
		QuestionAsker: asker,
		PhaseTimeouts: orchestrastate.PhaseTimeouts{BlockedEscalateS: 300},
	}
	r := barrierRunner(t, root, cfg)
	blocked := `{"status":"blocked","blocked_reason":"dependency_unmet"}`

	// First blocked result opens the window, no question yet.
	writeTimedState(t, root, "execution", time.Time{}, time.Time{})
	out := r.trackBlockedEscalation(context.Background(), blocked)
	if len(asker.asked) != 0 {
		t.Fatal("first blocked result must not escalate yet")
	}
	st, _, _ := orchestrastate.Load(root)
	if st.BlockedSince == "" {
		t.Fatal("blocked_since must be stamped")
	}
	if out != blocked {
		t.Fatalf("result must be unchanged on window open: %s", out)
	}

	// Blockage persists past the threshold → forced user question,
	// decision logged, window reset.
	writeTimedState(t, root, "execution", time.Time{}, time.Now().Add(-10*time.Minute))
	out = r.trackBlockedEscalation(context.Background(), blocked)
	if len(asker.asked) != 1 {
		t.Fatalf("escalation question expected, asked=%d", len(asker.asked))
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("result must stay JSON: %v", err)
	}
	if m["escalation_answer"] != "switch to phase: maintenance" {
		t.Fatalf("escalation_answer missing: %v", m)
	}
	st, _, _ = orchestrastate.Load(root)
	if st.BlockedSince != "" {
		t.Fatal("handled escalation must reset blocked_since")
	}

	// Non-blocked result clears an open window.
	writeTimedState(t, root, "execution", time.Time{}, time.Now())
	_ = r.trackBlockedEscalation(context.Background(), `{"status":"success"}`)
	st, _, _ = orchestrastate.Load(root)
	if st.BlockedSince != "" {
		t.Fatal("success must clear blocked_since")
	}

	// No QuestionAsker → advisory instead of a question.
	rootB := t.TempDir()
	rB := barrierRunner(t, rootB, ChildAgentConfig{PhaseTimeouts: orchestrastate.PhaseTimeouts{BlockedEscalateS: 300}})
	writeTimedState(t, rootB, "execution", time.Time{}, time.Now().Add(-10*time.Minute))
	outB := rB.trackBlockedEscalation(context.Background(), blocked)
	if !strings.Contains(outB, "blocked_escalation") {
		t.Fatalf("advisory expected without interactive channel: %s", outB)
	}
}
