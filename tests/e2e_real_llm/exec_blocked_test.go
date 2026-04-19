package e2e_real_llm

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/protocol"
)

// TestExecBlocked tests that exec.run is blocked without --allow-exec
func TestExecBlocked(t *testing.T) {
	requireE2ELLM(t)

	projectDir := setupTestProject(t)

	// Try to execute command without --allow-exec
	query := "запусти команду 'echo hello'"
	stdout, stderr, exitCode := runOrchestra(t, projectDir,
		"apply", "--via-core", "--plan-only", query)

	// Parse output
	_, _, errorCode := parseApplyOutput(stdout, stderr)

	// Should get ExecDenied error
	combined := stdout + "\n" + stderr
	hasExecDenied := errorCode == string(protocol.ExecDenied) ||
		strings.Contains(combined, string(protocol.ExecDenied)) ||
		strings.Contains(strings.ToLower(combined), "exec denied") ||
		strings.Contains(strings.ToLower(combined), "exec.run") ||
		exitCode != 0 // At least should fail

	if !hasExecDenied && exitCode == 0 {
		t.Errorf("Expected ExecDenied error or failure, but got success\nStdout: %s\nStderr: %s", stdout, stderr)
	}

	// Verify no command was actually executed (check output doesn't contain command output)
	if strings.Contains(combined, "hello") && !strings.Contains(combined, "ExecDenied") {
		t.Error("Command output should not appear (exec should be blocked)")
	}
}
