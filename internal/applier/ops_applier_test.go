package applier

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/ops"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/store"
)

func TestApplyOps_StrictMatch_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")

	original := "package main\n\nfunc a() {}\nfunc b() { old }\nfunc c() {}\n"
	if err := os.WriteFile(testFile, []byte(original), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	h := store.ComputeSHA256([]byte(original))

	expected := "func b() { old }"
	repl := "func b() { new }"

	op := ops.ReplaceRangeOp{
		Op:   ops.OpFileReplaceRange,
		Path: "test.go",
		Range: ops.Range{
			Start: ops.Position{Line: 3, Col: 0},
			End:   ops.Position{Line: 3, Col: len(expected)},
		},
		Expected:    expected,
		Replacement: repl,
		Conditions: ops.Conditions{
			FileHash:    h,
			AllowFuzzy:  true,
			FuzzyWindow: 2,
		},
	}

	result, err := ApplyOps(tmpDir, []ops.ReplaceRangeOp{op}, ApplyOptions{DryRun: true})
	if err != nil {
		t.Fatalf("ApplyOps failed: %v", err)
	}
	if len(result.Diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(result.Diffs))
	}

	if got := result.Diffs[0].Before; got != original {
		t.Fatalf("Before mismatch:\n%q\n!=\n%q", got, original)
	}
	if got := result.Diffs[0].After; got == original {
		t.Fatalf("After should differ from Before")
	}

	// Ensure file not modified in dry-run.
	b, _ := os.ReadFile(testFile)
	if string(b) != original {
		t.Fatalf("file was modified in dry-run mode")
	}
}

func TestApplyOps_StrictMatch_IgnoresFileHashMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")

	original := "package main\n\nfunc a() {}\nfunc b() { old }\nfunc c() {}\n"
	if err := os.WriteFile(testFile, []byte(original), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	expected := "func b() { old }"
	repl := "func b() { new }"

	// Deliberately wrong hash (simulates unrelated edits elsewhere in file between plan/apply).
	op := ops.ReplaceRangeOp{
		Op:   ops.OpFileReplaceRange,
		Path: "test.go",
		Range: ops.Range{
			Start: ops.Position{Line: 3, Col: 0},
			End:   ops.Position{Line: 3, Col: len(expected)},
		},
		Expected:    expected,
		Replacement: repl,
		Conditions: ops.Conditions{
			FileHash:    "sha256:deadbeef",
			AllowFuzzy:  false,
			FuzzyWindow: 0,
		},
	}

	_, err := ApplyOps(tmpDir, []ops.ReplaceRangeOp{op}, ApplyOptions{DryRun: true})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	coreErr, ok := protocol.AsError(err)
	if !ok {
		t.Fatalf("expected protocol.Error, got %T: %v", err, err)
	}
	if coreErr.Code != protocol.StaleContent {
		t.Fatalf("expected %s, got %s", protocol.StaleContent, coreErr.Code)
	}
}

func TestApplyOps_Stale_StrictMiss_FuzzyFindsUnique(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")

	original := "package main\n\nfunc a() {}\nfunc b() { old }\nfunc c() {}\n"
	if err := os.WriteFile(testFile, []byte(original), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Simulate file shifting lines after planning (insert a line above target).
	modified := "package main\n\nfunc a() {}\n// inserted\nfunc b() { old }\nfunc c() {}\n"
	if err := os.WriteFile(testFile, []byte(modified), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	expected := "func b() { old }"
	repl := "func b() { new }"

	// Start line points to the old location (line 3), but content moved to line 4.
	op := ops.ReplaceRangeOp{
		Op:   ops.OpFileReplaceRange,
		Path: "test.go",
		Range: ops.Range{
			Start: ops.Position{Line: 3, Col: 0},
			End:   ops.Position{Line: 3, Col: len(expected)},
		},
		Expected:    expected,
		Replacement: repl,
		Conditions: ops.Conditions{
			// Note: omit file_hash to allow fuzzy recovery in this unit test.
			// When file_hash is provided (e.g. --from-plan), we enforce it strictly.
			FileHash:    "",
			AllowFuzzy:  true,
			FuzzyWindow: 2,
		},
	}

	result, err := ApplyOps(tmpDir, []ops.ReplaceRangeOp{op}, ApplyOptions{DryRun: true})
	if err != nil {
		t.Fatalf("ApplyOps failed: %v", err)
	}
	if len(result.Diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(result.Diffs))
	}

	if got := result.Diffs[0].Before; got != modified {
		t.Fatalf("Before mismatch:\n%q\n!=\n%q", got, modified)
	}
	if got := result.Diffs[0].After; got == modified {
		t.Fatalf("After should differ from Before")
	}
	if got := result.Diffs[0].After; got == original {
		t.Fatalf("After should be based on current file content, not original")
	}
	if got := result.Diffs[0].After; got != "package main\n\nfunc a() {}\n// inserted\nfunc b() { new }\nfunc c() {}\n" {
		t.Fatalf("After mismatch:\n%q", got)
	}
}

func TestApplyOps_FuzzyAmbiguous_ReturnsAmbiguousMatch(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")

	content := "package main\nfunc a() { dup }\nfunc b() { dup }\n"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	h := store.ComputeSHA256([]byte(content))

	op := ops.ReplaceRangeOp{
		Op:   ops.OpFileReplaceRange,
		Path: "test.go",
		Range: ops.Range{
			Start: ops.Position{Line: 1, Col: 0}, // "fun"
			End:   ops.Position{Line: 1, Col: 3},
		},
		Expected:    "dup",
		Replacement: "xxx",
		Conditions: ops.Conditions{
			FileHash:    h,
			AllowFuzzy:  true,
			FuzzyWindow: 2,
		},
	}

	_, err := ApplyOps(tmpDir, []ops.ReplaceRangeOp{op}, ApplyOptions{DryRun: true})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	coreErr, ok := protocol.AsError(err)
	if !ok {
		t.Fatalf("expected protocol.Error, got %T: %v", err, err)
	}
	if coreErr.Code != protocol.AmbiguousMatch {
		t.Fatalf("expected code %s, got %s", protocol.AmbiguousMatch, coreErr.Code)
	}
}

func TestApplyOps_PathTraversal_Rejected(t *testing.T) {
	tmpDir := t.TempDir()

	op := ops.ReplaceRangeOp{
		Op:   ops.OpFileReplaceRange,
		Path: "../evil.go",
		Range: ops.Range{
			Start: ops.Position{Line: 0, Col: 0},
			End:   ops.Position{Line: 0, Col: 0},
		},
		Expected:    "",
		Replacement: "evil",
		Conditions: ops.Conditions{
			FileHash: store.ComputeSHA256(nil),
		},
	}

	_, err := ApplyOps(tmpDir, []ops.ReplaceRangeOp{op}, ApplyOptions{DryRun: true})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	coreErr, ok := protocol.AsError(err)
	if !ok {
		t.Fatalf("expected protocol.Error, got %T: %v", err, err)
	}
	if coreErr.Code != protocol.PathTraversal {
		t.Fatalf("expected code %s, got %s", protocol.PathTraversal, coreErr.Code)
	}
}

func TestApplyOps_CreateNewFile_FromEmpty(t *testing.T) {
	tmpDir := t.TempDir()

	emptyHash := store.ComputeSHA256(nil)
	op := ops.ReplaceRangeOp{
		Op:   ops.OpFileReplaceRange,
		Path: "new.go",
		Range: ops.Range{
			Start: ops.Position{Line: 0, Col: 0},
			End:   ops.Position{Line: 0, Col: 0},
		},
		Expected:    "",
		Replacement: "package main\n",
		Conditions: ops.Conditions{
			FileHash: emptyHash,
		},
	}

	_, err := ApplyOps(tmpDir, []ops.ReplaceRangeOp{op}, ApplyOptions{DryRun: false})
	if err != nil {
		t.Fatalf("ApplyOps failed: %v", err)
	}

	b, err := os.ReadFile(filepath.Join(tmpDir, "new.go"))
	if err != nil {
		t.Fatalf("read new file failed: %v", err)
	}
	if string(b) != "package main\n" {
		t.Fatalf("unexpected file content: %q", string(b))
	}
}

func TestApplyOps_Stale_DoesNotWriteOrBackup(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")

	original := "package main\n\nfunc a() {}\nfunc b() { old }\nfunc c() {}\n"
	if err := os.WriteFile(testFile, []byte(original), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	h := store.ComputeSHA256([]byte(original))

	// Wrong expected content -> strict fails; fuzzy disabled -> stale.
	expected := "func b() { DOES_NOT_MATCH }"
	op := ops.ReplaceRangeOp{
		Op:   ops.OpFileReplaceRange,
		Path: "test.go",
		Range: ops.Range{
			Start: ops.Position{Line: 3, Col: 0},
			End:   ops.Position{Line: 3, Col: len(expected)},
		},
		Expected:    expected,
		Replacement: "func b() { new }",
		Conditions: ops.Conditions{
			FileHash:    h,
			AllowFuzzy:  false,
			FuzzyWindow: 0,
		},
	}

	_, err := ApplyOps(tmpDir, []ops.ReplaceRangeOp{op}, ApplyOptions{DryRun: false, Backup: true, BackupSuffix: ".orchestra.bak"})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	coreErr, ok := protocol.AsError(err)
	if !ok {
		t.Fatalf("expected protocol.Error, got %T: %v", err, err)
	}
	if coreErr.Code != protocol.StaleContent {
		t.Fatalf("expected %s, got %s", protocol.StaleContent, coreErr.Code)
	}

	// File must remain unchanged and no backup should be created.
	b, _ := os.ReadFile(testFile)
	if string(b) != original {
		t.Fatalf("file content changed on stale error")
	}
	if _, statErr := os.Stat(testFile + ".orchestra.bak"); !os.IsNotExist(statErr) {
		t.Fatalf("backup should not be created on stale error")
	}
}

func TestApplyOps_SymlinkEscape_Rejected(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	// Create a dir symlink inside root that points outside.
	linkPath := filepath.Join(root, "linked")
	if err := os.Symlink(outside, linkPath); err != nil {
		t.Skipf("symlink not supported/allowed: %v", err)
	}

	op := ops.ReplaceRangeOp{
		Op:   ops.OpFileReplaceRange,
		Path: "linked/evil.go",
		Range: ops.Range{
			Start: ops.Position{Line: 0, Col: 0},
			End:   ops.Position{Line: 0, Col: 0},
		},
		Expected:    "",
		Replacement: "package main\n",
		Conditions: ops.Conditions{
			FileHash: store.ComputeSHA256(nil),
		},
	}

	_, err := ApplyOps(root, []ops.ReplaceRangeOp{op}, ApplyOptions{DryRun: false})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	coreErr, ok := protocol.AsError(err)
	if !ok {
		t.Fatalf("expected protocol.Error, got %T: %v", err, err)
	}
	if coreErr.Code != protocol.PathTraversal {
		t.Fatalf("expected %s, got %s", protocol.PathTraversal, coreErr.Code)
	}

	// Ensure we did not create a file outside the workspace.
	if _, statErr := os.Stat(filepath.Join(outside, "evil.go")); !os.IsNotExist(statErr) {
		t.Fatalf("expected no file created outside workspace")
	}
}
