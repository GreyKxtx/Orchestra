package tasks

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

func TestRunAcceptanceChecks_ConsentAndDryRun(t *testing.T) {
	checks := []AcceptanceCheck{{Cmd: "echo hi", ExpectExit: 0}}

	// Dry-run: skipped, not failed.
	out := runAcceptanceChecks(context.Background(), t.TempDir(), checks, true, true)
	if len(out) != 1 || !out[0].Skip || !out[0].OK {
		t.Fatalf("dry-run must skip: %+v", out)
	}

	// No exec consent: skipped, not failed.
	out = runAcceptanceChecks(context.Background(), t.TempDir(), checks, false, false)
	if len(out) != 1 || !out[0].Skip {
		t.Fatalf("no consent must skip: %+v", out)
	}
	if !strings.Contains(out[0].Detail, "allow-exec") {
		t.Fatalf("skip detail must explain consent: %q", out[0].Detail)
	}

	// Empty cmd: skipped.
	out = runAcceptanceChecks(context.Background(), t.TempDir(), []AcceptanceCheck{{Cmd: "  "}}, true, false)
	if len(out) != 1 || !out[0].Skip {
		t.Fatalf("empty cmd must skip: %+v", out)
	}

	// Skipped checks count as neither ran nor red.
	ran, green := acceptanceOutcome(out)
	if ran || !green {
		t.Fatalf("skipped checks: ran=%v green=%v", ran, green)
	}
}

func TestRunAcceptanceChecks_Execution(t *testing.T) {
	root := t.TempDir()

	// exit code + stdout match
	out := runAcceptanceChecks(context.Background(), root, []AcceptanceCheck{
		{Cmd: "echo orchestra", ExpectExit: 0, ExpectStdout: "orchestra"},
	}, true, false)
	if len(out) != 1 || out[0].Skip || !out[0].OK {
		t.Fatalf("green check expected: %+v", out)
	}

	// stdout mismatch → red
	out = runAcceptanceChecks(context.Background(), root, []AcceptanceCheck{
		{Cmd: "echo other", ExpectExit: 0, ExpectStdout: "orchestra"},
	}, true, false)
	if len(out) != 1 || out[0].OK {
		t.Fatalf("stdout mismatch must be red: %+v", out)
	}

	// exit code mismatch → red
	failCmd := "exit 3"
	if runtime.GOOS == "windows" {
		failCmd = "cmd /c exit 3"
	}
	out = runAcceptanceChecks(context.Background(), root, []AcceptanceCheck{
		{Cmd: failCmd, ExpectExit: 0},
	}, true, false)
	if len(out) != 1 || out[0].OK {
		t.Fatalf("exit mismatch must be red: %+v", out)
	}
	if !strings.Contains(out[0].Detail, "want 0") {
		t.Fatalf("detail must carry expected exit: %q", out[0].Detail)
	}

	ran, green := acceptanceOutcome(out)
	if !ran || green {
		t.Fatalf("red check: ran=%v green=%v", ran, green)
	}
}

func TestAcceptanceChecksFromGoal(t *testing.T) {
	// Plain-text goal → none.
	if got := acceptanceChecksFromGoal("just do it"); got != nil {
		t.Fatalf("plain goal: %+v", got)
	}
	// WorkOrder JSON with checks.
	goal := `{"intent":"x","target_files":["a.go"],"acceptance_checks":[{"cmd":"go test ./...","expect_exit":0}]}`
	got := acceptanceChecksFromGoal(goal)
	if len(got) != 1 || got[0].Cmd != "go test ./..." {
		t.Fatalf("parsed checks: %+v", got)
	}
	// Escalation suffix appended to the goal must not break parsing —
	// checks are extracted before the suffix is added, so a suffixed goal
	// simply yields none.
	if got := acceptanceChecksFromGoal(goal + "\n\n--- TIER ESCALATION ---"); got != nil {
		t.Fatalf("suffixed goal must not parse: %+v", got)
	}
}

func TestWrapWorkerNeedsReview(t *testing.T) {
	report := WorkerVerifyReport{Passed: true, Checks: []WorkerVerifyCheck{
		{Name: checkAcceptance, Path: "go test ./...", OK: true},
	}}
	out := wrapWorkerNeedsReview(`{"status":"success"}`, report, "## VERIFICATION FAILED\ncriterion X not met")
	if !strings.Contains(out, `"status":"needs_review"`) {
		t.Fatalf("needs_review status missing: %s", out)
	}
	if !strings.Contains(out, "llm_verifier_result") {
		t.Fatalf("verifier result missing: %s", out)
	}
}
