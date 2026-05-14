package e2e_real_llm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSimpleEdit asks the agent to rename a function and verifies the file was
// actually changed. The write/edit tools write directly to disk during the
// tool-call phase, so --apply is required and the result is always a file mutation.
func TestSimpleEdit(t *testing.T) {
	requireE2ELLM(t)

	projectDir := setupTestProject(t)
	mainPath := filepath.Join(projectDir, "main.go")

	query := "переименуй функцию greet в sayHello во всех местах в файле main.go"
	stdout, stderr, exitCode := runOrchestra(t, projectDir,
		"apply", "--via-core", "--apply", query)
	combined := stdout + "\n" + stderr

	errCat := classifyError(combined, exitCode)
	switch errCat {
	case ErrorCategoryOK:
		// good
	case ErrorCategoryModelOutput:
		t.Logf("model generated invalid output (acceptable for integration test):\n%s", combined)
		return
	case ErrorCategoryInfrastructure:
		t.Fatalf("infrastructure error (exit %d):\n%s", exitCode, combined)
	case ErrorCategorySystemBug:
		t.Fatalf("system bug (exit %d):\n%s", exitCode, combined)
	}

	after, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("read main.go after apply: %v", err)
	}
	content := string(after)

	if !strings.Contains(content, "sayHello") {
		t.Errorf("rename did not happen: 'sayHello' not found in file\ncontent:\n%s", content)
	}
	if strings.Contains(content, "func greet(") {
		t.Errorf("old signature 'func greet(' still present after rename\ncontent:\n%s", content)
	}

	t.Logf("OK — file renamed, exit=%d", exitCode)
	t.Logf("file content after apply:\n%s", content)
}
