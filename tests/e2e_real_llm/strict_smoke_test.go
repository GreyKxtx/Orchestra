package e2e_real_llm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSmokeCLI_Strict is a "real agent" smoke-check:
// the run must complete without infrastructure or system errors.
// Note: --plan-only does not prevent tool-based writes (write/edit tools
// write directly to disk during tool-call phase), so file mutation is
// possible and is not treated as a failure here.
func TestSmokeCLI_Strict(t *testing.T) {
	requireE2ELLM(t)

	projectDir := setupTestProject(t)

	query := "добавь комментарий // Hello в начало функции main"
	stdout, stderr, exitCode := runOrchestra(t, projectDir,
		"apply", "--via-core", "--plan-only", query)

	combined := stdout + "\n" + stderr
	errCat := classifyError(combined, exitCode)

	switch errCat {
	case ErrorCategoryInfrastructure:
		t.Fatalf("infrastructure error (exit %d):\n%s", exitCode, combined)
	case ErrorCategorySystemBug:
		t.Fatalf("system bug (exit %d):\n%s", exitCode, combined)
	}

	if exitCode != 0 {
		t.Fatalf("expected success (exit 0), got exit=%d\nStdout: %s\nStderr: %s", exitCode, stdout, stderr)
	}

	if strings.Contains(combined, "error_code=") {
		t.Fatalf("unexpected error_code in output:\n%s", combined)
	}

	// Artifacts must exist.
	planPath := filepath.Join(projectDir, ".orchestra", "plan.json")
	diffPath := filepath.Join(projectDir, ".orchestra", "diff.txt")
	if _, err := os.Stat(planPath); err != nil {
		t.Fatalf("expected plan artifact %s: %v", planPath, err)
	}
	if _, err := os.Stat(diffPath); err != nil {
		t.Fatalf("expected diff artifact %s: %v", diffPath, err)
	}

	t.Logf("OK: plan-only succeeded, artifacts exist, exit=%d", exitCode)
}
