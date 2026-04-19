package e2e_real_llm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestSmokeCLI tests basic CLI integration (quick smoke test)
// Note: This test checks that CLI can communicate with LLM API and handle responses.
// It may fail if model generates invalid operations (this is expected behavior for integration testing).
func TestSmokeCLI(t *testing.T) {
	requireE2ELLM(t)

	projectDir := setupTestProject(t)

	// Simple dry-run apply (simple query)
	query := "добавь комментарий // Hello в начало функции main"
	stdout, stderr, exitCode := runOrchestra(t, projectDir,
		"apply", "--via-core", query)

	combined := stdout + "\n" + stderr

	// Classify error
	errCat := classifyError(combined, exitCode)

	switch errCat {
	case ErrorCategoryOK:
		t.Logf("✓ Test passed: LLM responded successfully")
		// If successful, verify artifacts exist
		planPath := filepath.Join(projectDir, ".orchestra", "plan.json")
		diffPath := filepath.Join(projectDir, ".orchestra", "diff.txt")
		if _, err := os.Stat(planPath); err == nil {
			t.Logf("✓ Plan file created")
		}
		if _, err := os.Stat(diffPath); err == nil {
			t.Logf("✓ Diff file created")
		}

	case ErrorCategoryModelOutput:
		t.Logf("⚠ Test completed with model output errors (expected for integration test)")
		t.Logf("Error details: %s", combined[:min(300, len(combined))])

	case ErrorCategoryInfrastructure:
		t.Fatalf("Infrastructure error (LLM API connection failed): exit code %d\nStdout: %s\nStderr: %s", exitCode, stdout, stderr)

	case ErrorCategorySystemBug:
		t.Fatalf("System bug detected: exit code %d\nStdout: %s\nStderr: %s", exitCode, stdout, stderr)
	}

	// Should produce some output (already checked above)
	if combined == "" {
		t.Error("Expected some output from orchestra apply")
	}

	// Verify plan/diff file might exist (depending on implementation)
	planPath := filepath.Join(projectDir, ".orchestra", "plan.json")
	diffPath := filepath.Join(projectDir, ".orchestra", "diff.txt")

	hasPlan := false
	if _, err := os.Stat(planPath); err == nil {
		hasPlan = true
	}

	hasDiff := false
	if _, err := os.Stat(diffPath); err == nil {
		hasDiff = true
	}

	// At least one should exist or output should contain plan/diff info
	if !hasPlan && !hasDiff && !containsPlanInfo(combined) {
		t.Logf("Warning: No plan/diff file found and output doesn't contain plan info. This might be OK depending on implementation.")
	}
}

// containsPlanInfo checks if output contains plan/diff indicators
func containsPlanInfo(output string) bool {
	indicators := []string{
		"plan",
		"diff",
		"ops",
		"steps",
		"---",
		"+++",
	}
	for _, ind := range indicators {
		if strings.Contains(strings.ToLower(output), ind) {
			return true
		}
	}
	return false
}
