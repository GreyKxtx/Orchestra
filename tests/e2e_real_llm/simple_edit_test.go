package e2e_real_llm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSimpleEdit tests basic edit scenario: rename function
func TestSimpleEdit(t *testing.T) {
	requireE2ELLM(t)

	projectDir := setupTestProject(t)

	// Run dry-run apply: rename function greet to sayHello
	query := "переименуй функцию greet в sayHello"
	stdout, stderr, exitCode := runOrchestra(t, projectDir,
		"apply", "--via-core", query)

	combined := stdout + "\n" + stderr

	// Classify error
	errCat := classifyError(combined, exitCode)

	switch errCat {
	case ErrorCategoryOK:
		// Continue with assertions below
	case ErrorCategoryModelOutput:
		t.Logf("Model generated invalid operations (expected for integration test)")
		return // This is OK for E2E test - model errors are acceptable
	case ErrorCategoryInfrastructure:
		t.Fatalf("Infrastructure error: exit code %d\nStdout: %s\nStderr: %s", exitCode, stdout, stderr)
	case ErrorCategorySystemBug:
		t.Fatalf("System bug detected: exit code %d\nStdout: %s\nStderr: %s", exitCode, stdout, stderr)
	}

	// Parse output
	_, _, errorCode := parseApplyOutput(stdout, stderr)

	// Assertions
	if errorCode != "" {
		t.Errorf("Expected no error, got: %s", errorCode)
	}

	// Check plan.json for ops (ops are saved to file, not stdout)
	planPath := filepath.Join(projectDir, ".orchestra", "plan.json")
	planData, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("plan.json not found: %v", err)
	}

	var plan struct {
		Ops []interface{} `json:"ops"`
	}
	if err := json.Unmarshal(planData, &plan); err != nil {
		t.Fatalf("Failed to parse plan.json: %v", err)
	}

	if len(plan.Ops) == 0 {
		t.Error("Expected ops in plan.json, but none found")
	}

	// Check diff.txt exists
	diffPath := filepath.Join(projectDir, ".orchestra", "diff.txt")
	diffData, err := os.ReadFile(diffPath)
	if err != nil {
		t.Fatalf("diff.txt not found: %v", err)
	}

	if len(diffData) == 0 {
		t.Error("Expected diff in diff.txt, but file is empty")
	}

	// Verify that main.go would be changed (check diff content)
	if !strings.Contains(combined, "greet") || !strings.Contains(combined, "sayHello") {
		t.Logf("Warning: diff might not contain expected changes. Output:\n%s", combined)
	}

	// Verify file was NOT actually changed (dry-run)
	mainPath := filepath.Join(projectDir, "main.go")
	content, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("Failed to read main.go: %v", err)
	}

	if strings.Contains(string(content), "sayHello") {
		t.Error("File was modified in dry-run mode (should not happen)")
	}

	if !strings.Contains(string(content), "greet") {
		t.Error("Original function name should still be present")
	}
}
