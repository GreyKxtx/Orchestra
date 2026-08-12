package agent

import (
	"strings"
	"testing"
)

func TestValidBlockedReason(t *testing.T) {
	for _, ok := range []string{"stale_contract", "missing_answer", "verify_failed", "permission_denied", "tier_exhausted", "dependency_unmet", " Verify_Failed "} {
		if !ValidBlockedReason(ok) {
			t.Fatalf("%q must be valid", ok)
		}
	}
	for _, bad := range []string{"", "unknown", "blocked", "stale contract"} {
		if ValidBlockedReason(bad) {
			t.Fatalf("%q must be invalid", bad)
		}
	}
}

func TestCheckBlockedReasonTaxonomy(t *testing.T) {
	// Non-JSON, no field, valid value → pass.
	for _, ok := range []string{"", "plain text", `{"status":"done"}`, `{"status":"blocked","blocked_reason":"stale_contract"}`} {
		if err := checkBlockedReasonTaxonomy(ok); err != nil {
			t.Fatalf("%q: unexpected error %v", ok, err)
		}
	}
	err := checkBlockedReasonTaxonomy(`{"status":"blocked","blocked_reason":"i dunno"}`)
	if err == nil {
		t.Fatal("free-text blocked_reason must be rejected")
	}
	if !strings.Contains(err.Error(), "stale_contract") {
		t.Fatalf("error must list the closed taxonomy: %v", err)
	}
}
