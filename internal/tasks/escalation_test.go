package tasks

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/llm"
)

func TestTierEscalationSettingsDefaults(t *testing.T) {
	var s TierEscalationSettings
	if s.baseRounds() != 2 {
		t.Fatalf("baseRounds default = %d, want 2", s.baseRounds())
	}
	if s.escalatedRounds() != 1 {
		t.Fatalf("escalatedRounds default = %d, want 1", s.escalatedRounds())
	}
	if s.tierName() != "complex" {
		t.Fatalf("tierName default = %q, want complex", s.tierName())
	}
	s = TierEscalationSettings{FailuresBeforeEscalation: 3, MaxEscalatedRetries: 2, EscalationTier: "L4"}
	if s.baseRounds() != 3 || s.escalatedRounds() != 2 || s.tierName() != "L4" {
		t.Fatalf("explicit settings not honored: %+v", s)
	}
}

func TestWorkerVerificationFailure(t *testing.T) {
	// Deterministic failure → escalate, summary carries the check detail.
	failed := `{"status":"verification_failed","verification":{"passed":false,"checks":[{"name":"go_build","path":"./x","ok":false,"detail":"undefined: Foo"}]}}`
	summary, ok := workerVerificationFailure(failed)
	if !ok {
		t.Fatal("verification_failed must trigger escalation")
	}
	if !strings.Contains(summary, "go_build") || !strings.Contains(summary, "undefined: Foo") {
		t.Fatalf("summary missing failure detail: %q", summary)
	}

	// Everything else must not escalate.
	for _, raw := range []string{
		"",
		"not json at all",                        // malformed output ≠ quality signal
		`{"status":"verified_success"}`,          // success
		`{"status":"llm_verification_failed"}`,   // criteria dispute → Lead decides
		`{"status":"error","message":"timeout"}`, // infra error
	} {
		if _, ok := workerVerificationFailure(raw); ok {
			t.Errorf("%q must not trigger escalation", raw)
		}
	}
}

func TestAnnotateEscalatedResult(t *testing.T) {
	out := annotateEscalatedResult(`{"status":"verified_success"}`, "complex", "gpt-large")
	if !strings.Contains(out, `"escalated_to_tier":"complex"`) || !strings.Contains(out, `"escalated_model":"gpt-large"`) {
		t.Fatalf("annotation missing: %s", out)
	}
	// Non-object payloads pass through untouched.
	if got := annotateEscalatedResult("plain text", "complex", "m"); got != "plain text" {
		t.Fatalf("non-JSON must pass through, got %q", got)
	}
}

func TestEscalationClientResolution(t *testing.T) {
	// No resolvers → not ok.
	r := &TaskRunner{child: ChildAgentConfig{}}
	if _, _, _, ok := r.escalationClient(); ok {
		t.Fatal("no resolvers must yield ok=false")
	}
	// Tier resolves and client builds → ok with labels.
	r = &TaskRunner{child: ChildAgentConfig{
		TierEscalation: TierEscalationSettings{EscalationTier: "complex"},
		ResolveTier: func(tier string) (string, string, bool) {
			if tier != "complex" {
				t.Fatalf("unexpected tier %q", tier)
			}
			return "openai", "big-model", true
		},
		ResolveClient: func(provider, model string) (llm.Client, string, string, error) {
			return &mockTaskResultLLM{result: "x"}, provider, model, nil
		},
	}}
	client, pl, ml, ok := r.escalationClient()
	if !ok || client == nil || pl != "openai" || ml != "big-model" {
		t.Fatalf("escalation client resolution failed: ok=%v pl=%q ml=%q", ok, pl, ml)
	}
}
