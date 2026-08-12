package tasks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/tools/exec"
)

// Acceptance checks (spec §5.4, ADR-4): the machine-executable half of the
// WorkOrder acceptance criteria. The runtime executes them — the model never
// self-assesses. Execution follows the exec.run consent policy: without
// --allow-exec (or exec.confirm: false) checks are skipped, not failed, and
// the skip is visible in the report.

const acceptanceCheckTimeout = 120 * time.Second

// checkAcceptance is the WorkerVerifyCheck name for acceptance probes.
const checkAcceptance = "acceptance_check"

// runAcceptanceChecks executes the WorkOrder's acceptance_checks sequentially
// and returns them as verify-report entries.
func runAcceptanceChecks(ctx context.Context, root string, checks []AcceptanceCheck, execAllowed, dryRun bool) []WorkerVerifyCheck {
	out := make([]WorkerVerifyCheck, 0, len(checks))
	for _, ac := range checks {
		cmd := strings.TrimSpace(ac.Cmd)
		c := WorkerVerifyCheck{Name: checkAcceptance, Path: cmd, OK: true}
		switch {
		case cmd == "":
			c.Skip = true
			c.Detail = "empty cmd"
		case dryRun:
			c.Skip = true
			c.Detail = "skipped in dry-run (staged changes are not on disk)"
		case !execAllowed:
			c.Skip = true
			c.Detail = "skipped: exec consent required (--allow-exec or exec.confirm: false)"
		default:
			evalAcceptanceCheck(ctx, root, ac, &c)
		}
		out = append(out, c)
	}
	return out
}

func evalAcceptanceCheck(ctx context.Context, root string, ac AcceptanceCheck, c *WorkerVerifyCheck) {
	resp, err := exec.Run(ctx, root, acceptanceCheckTimeout, 32*1024, exec.RunRequest{
		Command:   strings.TrimSpace(ac.Cmd),
		TimeoutMS: int(acceptanceCheckTimeout / time.Millisecond),
	})
	if err != nil {
		c.OK = false
		c.Detail = err.Error()
		return
	}
	if resp == nil {
		c.OK = false
		c.Detail = "no response from exec"
		return
	}
	if resp.ExitCode != ac.ExpectExit {
		c.OK = false
		c.Detail = fmt.Sprintf("exit %d, want %d; %s", resp.ExitCode, ac.ExpectExit, compactOutput(resp.Stderr, resp.Stdout))
		return
	}
	if want := strings.TrimSpace(ac.ExpectStdout); want != "" && !strings.Contains(resp.Stdout, want) {
		c.OK = false
		c.Detail = fmt.Sprintf("stdout does not contain %q; got: %s", want, compactOutput(resp.Stdout, ""))
	}
}

func compactOutput(primary, fallback string) string {
	out := strings.TrimSpace(primary)
	if out == "" {
		out = strings.TrimSpace(fallback)
	}
	if len(out) > 400 {
		out = out[:400] + "..."
	}
	return out
}

// acceptanceOutcome summarizes executed acceptance checks: ran reports that at
// least one check actually executed (not skipped); green means none failed.
func acceptanceOutcome(checks []WorkerVerifyCheck) (ran, green bool) {
	green = true
	for _, c := range checks {
		if c.Name != checkAcceptance {
			continue
		}
		if c.Skip {
			continue
		}
		ran = true
		if !c.OK {
			green = false
		}
	}
	return ran, green
}

// acceptanceChecksFromGoal extracts acceptance_checks when the worker goal is
// a WorkOrder JSON; plain-text goals have none.
func acceptanceChecksFromGoal(childGoal string) []AcceptanceCheck {
	goal := strings.TrimSpace(childGoal)
	if goal == "" || !strings.HasPrefix(goal, "{") {
		return nil
	}
	wo, err := ParseWorkOrderJSON(goal)
	if err != nil || wo == nil {
		return nil
	}
	return wo.AcceptanceChecks
}
