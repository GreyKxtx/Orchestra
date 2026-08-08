package e2e_agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
)

// TestStagingApply_E2E_DryRunThenApply verifies dry-run staging → explicit apply
// leaves disk matching staged content with backup (acceptance §11 apply path).
func TestStagingApply_E2E_DryRunThenApply(t *testing.T) {
	root, initialHash := workerLSPFixture(t)
	original, err := os.ReadFile(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatal(err)
	}

	tr := newLSPFixRunner(t, root)

	// Simulate Worker fix: remove a hypothetical bad line via staged edit path.
	_, err = tr.FSEdit(context.Background(), tools.FSEditRequest{
		Path:     "main.go",
		Search:   "func Foo() {}",
		Replace:  "func Foo() {}\n\n// staging-apply-e2e",
		FileHash: initialHash,
	})
	if err != nil {
		t.Fatalf("FSEdit: %v", err)
	}

	staged := tr.StagedOps()
	if len(staged) == 0 {
		t.Fatal("expected staged ops")
	}

	onDiskBefore, _ := os.ReadFile(filepath.Join(root, "main.go"))
	if string(onDiskBefore) != string(original) {
		t.Fatal("dry-run must not write before apply")
	}

	applyResp, err := tr.FSApplyOps(context.Background(), tools.FSApplyOpsRequest{
		Ops:    staged,
		DryRun: false,
		Backup: true,
	})
	if err != nil {
		t.Fatalf("FSApplyOps: %v", err)
	}
	if len(applyResp.Diffs) == 0 {
		t.Fatal("expected diffs from apply")
	}

	after, err := os.ReadFile(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), "staging-apply-e2e") {
		t.Fatalf("disk missing staged change: %q", string(after))
	}
	if _, err := os.Stat(filepath.Join(root, "main.go.orchestra.bak")); err != nil {
		t.Fatalf("expected backup file: %v", err)
	}
}
