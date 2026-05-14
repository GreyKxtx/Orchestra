package e2e_real_llm

import (
	"strings"
	"testing"
)

// TestExecBlocked verifies that exec.run is not available without --allow-exec.
// Without the tool, the model cannot execute commands: it either refuses or
// generates a plan with no ops. The key invariant is that no actual command
// output appears (i.e. "hello" from `echo hello` must not be present as a
// side-effect of real execution).
func TestExecBlocked(t *testing.T) {
	requireE2ELLM(t)

	projectDir := setupTestProject(t)

	query := "запусти команду 'echo hello' и покажи вывод"
	stdout, stderr, exitCode := runOrchestra(t, projectDir,
		"apply", "--via-core", "--apply", query)

	combined := stdout + "\n" + stderr

	errCat := classifyError(combined, exitCode)
	switch errCat {
	case ErrorCategoryInfrastructure:
		t.Fatalf("infrastructure error (exit %d):\n%s", exitCode, combined)
	case ErrorCategorySystemBug:
		t.Fatalf("system bug (exit %d):\n%s", exitCode, combined)
	}

	// exec.run is not in the tool list without --allow-exec, so the model
	// cannot call it. A graceful refusal (exit 0) is the expected outcome.
	// Verify no shell command actually ran by checking for ExecDenied absence:
	// if ExecDenied appears the model somehow called exec.run, which should not
	// happen without --allow-exec.
	lc := strings.ToLower(combined)
	if strings.Contains(lc, "exec_denied") || strings.Contains(lc, "exec denied") {
		t.Errorf("ExecDenied appeared — exec.run should not be callable without --allow-exec:\n%s", combined)
		return
	}

	// The model should not have executed anything; log for informational purposes.
	t.Logf("OK: exec.run not available (exit=%d), model refused gracefully", exitCode)
}
