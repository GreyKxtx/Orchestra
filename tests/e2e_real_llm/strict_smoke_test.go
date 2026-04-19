package e2e_real_llm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSmokeCLI_Strict is a "real agent" smoke-check:
// the run must reach final patches and complete without model-output errors.
//
// Enabled only with ORCH_E2E_LLM=1.
func TestSmokeCLI_Strict(t *testing.T) {
	requireE2ELLM(t)

	projectDir := setupTestProject(t)

	// Dry-run to ensure no writes.
	query := "добавь комментарий // Hello в начало функции main"
	stdout, stderr, exitCode := runOrchestra(t, projectDir,
		"apply", "--via-core", "--plan-only", query)

	if exitCode != 0 {
		t.Fatalf("expected success (exit 0), got exit=%d\nStdout: %s\nStderr: %s", exitCode, stdout, stderr)
	}

	combined := stdout + "\n" + stderr
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

	// Ensure file not modified in dry-run mode.
	mainPath := filepath.Join(projectDir, "main.go")
	content, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if strings.Contains(string(content), "// Hello") {
		t.Fatalf("file was modified in --plan-only mode (should not happen)")
	}
}
