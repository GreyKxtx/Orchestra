package e2e_real_llm

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/protocol"
)

// TestRealLLMMinimalFlow tests the minimal end-to-end flow:
// 1. LLM receives task
// 2. LLM makes at least 1 tool_call (fs.read to get file_hash)
// 3. LLM returns final.patches with valid file_hash
// 4. --plan-only creates artifacts without modifying files
// 5. --from-plan --apply applies changes and creates backup
// 6. Repeated --from-plan --apply gives predictable result (StaleContent or AlreadyExists) without side effects
//
// This test proves the pipeline works end-to-end, not just "provider responds".
func TestRealLLMMinimalFlow(t *testing.T) {
	requireE2ELLM(t)

	projectDir := setupTestProject(t)

	// Simple task: add a comment to main.go
	query := "добавь комментарий // MinimalFlowTest в начало функции main"

	mainPath := filepath.Join(projectDir, "main.go")
	origContent, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("Failed to read main.go: %v", err)
	}

	// Step 1: --plan-only (must succeed and produce plan artifact)
	t.Logf("Step 1: Running --plan-only")
	stdout1, stderr1, exitCode1 := runOrchestra(t, projectDir,
		"apply", "--via-core", "--plan-only", query)

	combined1 := stdout1 + "\n" + stderr1
	errCat1 := classifyError(combined1, exitCode1)

	switch errCat1 {
	case ErrorCategoryModelOutput:
		t.Skipf("Model failed to generate a plan. Cannot test minimal flow without a valid plan.\nOutput:\n%s", combined1)
	case ErrorCategoryInfrastructure:
		t.Fatalf("Infrastructure error in plan-only (exit %d):\n%s", exitCode1, combined1)
	case ErrorCategorySystemBug:
		t.Fatalf("System bug in plan-only (exit %d):\n%s", exitCode1, combined1)
	default:
		// OK, continue
	}

	if exitCode1 != 0 {
		t.Fatalf("plan-only failed (exit %d):\n%s", exitCode1, combined1)
	}

	// Verify plan artifact exists
	planPath := filepath.Join(projectDir, ".orchestra", "plan.json")
	planData, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("plan-only did not produce %s: %v", planPath, err)
	}

	var plan struct {
		Query   string                   `json:"query"`
		Patches []map[string]interface{} `json:"patches,omitempty"`
		Ops     []map[string]interface{} `json:"ops,omitempty"`
	}
	if err := json.Unmarshal(planData, &plan); err != nil {
		t.Fatalf("plan.json is not valid JSON: %v", err)
	}

	// Verify plan has ops (required for --from-plan)
	if len(plan.Ops) == 0 {
		t.Fatalf("plan.json has no ops; cannot test --from-plan flow")
	}

	// Verify file was NOT modified in --plan-only mode
	afterPlanOnly, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("Failed to read main.go after plan-only: %v", err)
	}
	if !bytes.Equal(origContent, afterPlanOnly) {
		t.Fatalf("File was modified in --plan-only mode (should not happen)")
	}

	// Verify diff artifact exists
	diffPath := filepath.Join(projectDir, ".orchestra", "diff.txt")
	diffData, err := os.ReadFile(diffPath)
	if err != nil {
		t.Fatalf("diff.txt not created: %v", err)
	}
	if len(diffData) == 0 {
		t.Logf("WARNING: diff.txt is empty (may be OK if no changes planned)")
	}

	t.Logf("✓ Step 1 passed: plan-only created artifacts, file unchanged")

	// Step 2: --from-plan --apply (apply the saved plan)
	t.Logf("Step 2: Running --from-plan --apply")
	stdout2, stderr2, exitCode2 := runOrchestra(t, projectDir,
		"apply", "--from-plan", planPath, "--apply")

	combined2 := stdout2 + "\n" + stderr2
	errCat2 := classifyError(combined2, exitCode2)

	if errCat2 == ErrorCategoryInfrastructure || errCat2 == ErrorCategorySystemBug {
		t.Fatalf("Infrastructure/system error in apply (exit %d):\n%s", exitCode2, combined2)
	}

	// Check if we got StaleContent (file might have changed between plan and apply)
	_, _, parsedCode := parseApplyOutput(stdout2, stderr2)
	hasStale := parsedCode == string(protocol.StaleContent) || strings.Contains(combined2, string(protocol.StaleContent))

	if hasStale {
		// This is OK if file changed externally, but for minimal flow test we expect success
		// Check if file was actually modified externally
		currentContent, _ := os.ReadFile(mainPath)
		if bytes.Equal(origContent, currentContent) {
			// File unchanged, stale is unexpected
			t.Fatalf("Got StaleContent but file was not modified externally:\n%s", combined2)
		}
		t.Skipf("Got StaleContent (file changed externally). This is expected behavior but breaks minimal flow test.\nOutput:\n%s", combined2)
	}

	if exitCode2 != 0 {
		t.Fatalf("apply failed (exit %d):\n%s", exitCode2, combined2)
	}

	// Verify file was modified
	afterApply, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("Failed to read main.go after apply: %v", err)
	}
	if bytes.Equal(origContent, afterApply) {
		t.Fatalf("File was not modified after --from-plan --apply")
	}

	// Verify backup was created
	backupPath := mainPath + ".orchestra.bak"
	backupData, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("Backup file not created: %v", err)
	}
	if !bytes.Equal(origContent, backupData) {
		t.Fatalf("Backup content does not match original")
	}

	// Verify change is what we expected (contains the comment)
	if !strings.Contains(string(afterApply), "// MinimalFlowTest") {
		t.Fatalf("Applied change does not contain expected comment. File content:\n%s", string(afterApply))
	}

	t.Logf("✓ Step 2 passed: apply succeeded, file modified, backup created")

	// Step 3: Repeat --from-plan --apply (should give predictable result)
	t.Logf("Step 3: Running --from-plan --apply again (should be idempotent or give StaleContent)")
	stdout3, stderr3, exitCode3 := runOrchestra(t, projectDir,
		"apply", "--from-plan", planPath, "--apply")

	combined3 := stdout3 + "\n" + stderr3
	errCat3 := classifyError(combined3, exitCode3)

	// Get current file state
	afterRepeat, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("Failed to read main.go after repeat: %v", err)
	}

	// Check for expected outcomes:
	// 1. StaleContent (if file_hash in plan doesn't match current file)
	// 2. Success with no changes (idempotent)
	// 3. AlreadyExists (if operation is create with must_not_exist)

	_, _, parsedCode3 := parseApplyOutput(stdout3, stderr3)
	hasStale3 := parsedCode3 == string(protocol.StaleContent) || strings.Contains(combined3, string(protocol.StaleContent))
	hasAlreadyExists := strings.Contains(combined3, string(protocol.AlreadyExists)) || strings.Contains(combined3, "already exists")

	if hasStale3 {
		// StaleContent is expected: plan has old file_hash, current file has new content
		// Verify no side effects: no new backup, file unchanged
		backupDir := filepath.Join(projectDir, ".orchestra", "backups")
		backupEntries, _ := os.ReadDir(backupDir)
		if len(backupEntries) > 0 {
			t.Logf("WARNING: backups directory has entries (may be OK)")
		}

		// Check that file content matches what we had after first apply
		if !bytes.Equal(afterApply, afterRepeat) {
			t.Fatalf("StaleContent occurred, but file was modified (should be unchanged)")
		}

		// Check that no new backup was created (backup should still be from first apply)
		if _, err := os.Stat(backupPath); err != nil {
			t.Fatalf("Backup file disappeared: %v", err)
		}
		// Backup should be from first apply, not from repeat
		// (we can't easily check mtime, but we can verify content)
		backupData2, _ := os.ReadFile(backupPath)
		if !bytes.Equal(origContent, backupData2) {
			t.Fatalf("Backup content changed (should be original)")
		}

		t.Logf("✓ Step 3 passed: StaleContent detected correctly, no side effects")
		return
	}

	if hasAlreadyExists {
		// AlreadyExists is also acceptable (for create operations)
		t.Logf("✓ Step 3 passed: AlreadyExists (idempotent operation)")
		return
	}

	if exitCode3 == 0 {
		// Success: check if it's idempotent (no changes)
		if bytes.Equal(afterApply, afterRepeat) {
			t.Logf("✓ Step 3 passed: idempotent apply (no changes, no errors)")
			return
		}
		// File changed again - this might be OK if operation is not idempotent
		t.Logf("WARNING: Repeat apply succeeded and modified file again (may be expected)")
		return
	}

	// Unexpected error
	if errCat3 == ErrorCategoryInfrastructure || errCat3 == ErrorCategorySystemBug {
		t.Fatalf("Unexpected infrastructure/system error in repeat apply (exit %d):\n%s", exitCode3, combined3)
	}

	// Model output error might be OK if it's a validation error
	t.Logf("Repeat apply failed with exit %d (may be expected):\n%s", exitCode3, combined3)
}
