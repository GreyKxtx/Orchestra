package applier

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/ops"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/cache"
)

func TestApplyOps_StrictMatch_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")

	original := "package main\n\nfunc a() {}\nfunc b() { old }\nfunc c() {}\n"
	if err := os.WriteFile(testFile, []byte(original), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	h := cache.ComputeSHA256([]byte(original))

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
	h := cache.ComputeSHA256([]byte(content))

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
			FileHash: cache.ComputeSHA256(nil),
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

	emptyHash := cache.ComputeSHA256(nil)
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

	h := cache.ComputeSHA256([]byte(original))

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
			FileHash: cache.ComputeSHA256(nil),
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

// ---- ApplyAnyOps: file.write_atomic ----

func TestApplyAnyOps_WriteAtomic_NewFile(t *testing.T) {
	root := t.TempDir()

	anyOp := ops.AnyOp{
		Op:   ops.OpFileWriteAtomic,
		Path: "hello.go",
		WriteAtomic: &ops.WriteAtomicOp{
			Op:      ops.OpFileWriteAtomic,
			Path:    "hello.go",
			Content: "package main\n",
		},
	}

	result, err := ApplyAnyOps(root, []ops.AnyOp{anyOp}, ApplyOptions{DryRun: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ChangedFiles) != 1 || result.ChangedFiles[0] != "hello.go" {
		t.Errorf("ChangedFiles: %v", result.ChangedFiles)
	}

	b, err := os.ReadFile(filepath.Join(root, "hello.go"))
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if string(b) != "package main\n" {
		t.Errorf("content: %q", string(b))
	}
}

func TestApplyAnyOps_WriteAtomic_DryRun(t *testing.T) {
	root := t.TempDir()

	anyOp := ops.AnyOp{
		Op:   ops.OpFileWriteAtomic,
		Path: "x.go",
		WriteAtomic: &ops.WriteAtomicOp{
			Op:      ops.OpFileWriteAtomic,
			Path:    "x.go",
			Content: "package main\n",
		},
	}

	_, err := ApplyAnyOps(root, []ops.AnyOp{anyOp}, ApplyOptions{DryRun: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "x.go")); !os.IsNotExist(statErr) {
		t.Fatal("file should not be created in dry-run")
	}
}

func TestApplyAnyOps_WriteAtomic_MustNotExist_Fails(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "exists.go")
	if err := os.WriteFile(existing, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	anyOp := ops.AnyOp{
		Op:   ops.OpFileWriteAtomic,
		Path: "exists.go",
		WriteAtomic: &ops.WriteAtomicOp{
			Op:      ops.OpFileWriteAtomic,
			Path:    "exists.go",
			Content: "new",
			Conditions: ops.WriteAtomicConditions{
				MustNotExist: true,
			},
		},
	}

	_, err := ApplyAnyOps(root, []ops.AnyOp{anyOp}, ApplyOptions{DryRun: false})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	coreErr, ok := protocol.AsError(err)
	if !ok {
		t.Fatalf("expected protocol.Error, got %T: %v", err, err)
	}
	if coreErr.Code != protocol.AlreadyExists {
		t.Errorf("expected AlreadyExists, got %s", coreErr.Code)
	}
}

func TestApplyAnyOps_WriteAtomic_HashMismatch(t *testing.T) {
	root := t.TempDir()
	f := filepath.Join(root, "f.go")
	if err := os.WriteFile(f, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	anyOp := ops.AnyOp{
		Op:   ops.OpFileWriteAtomic,
		Path: "f.go",
		WriteAtomic: &ops.WriteAtomicOp{
			Op:      ops.OpFileWriteAtomic,
			Path:    "f.go",
			Content: "new content\n",
			Conditions: ops.WriteAtomicConditions{
				FileHash: "sha256:deadbeef",
			},
		},
	}

	_, err := ApplyAnyOps(root, []ops.AnyOp{anyOp}, ApplyOptions{DryRun: false})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	coreErr, ok := protocol.AsError(err)
	if !ok {
		t.Fatalf("expected protocol.Error, got %T: %v", err, err)
	}
	if coreErr.Code != protocol.StaleContent {
		t.Errorf("expected StaleContent, got %s", coreErr.Code)
	}
}

func TestApplyAnyOps_ConflictingOps_SamePath(t *testing.T) {
	root := t.TempDir()
	f := filepath.Join(root, "conflict.go")
	if err := os.WriteFile(f, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	replaceOp := ops.AnyOp{
		Op:   ops.OpFileReplaceRange,
		Path: "conflict.go",
		ReplaceRange: &ops.ReplaceRangeOp{
			Op:          ops.OpFileReplaceRange,
			Path:        "conflict.go",
			Range:       ops.Range{Start: ops.Position{Line: 0, Col: 0}, End: ops.Position{Line: 0, Col: 12}},
			Expected:    "package main",
			Replacement: "package foo",
		},
	}
	writeOp := ops.AnyOp{
		Op:   ops.OpFileWriteAtomic,
		Path: "conflict.go",
		WriteAtomic: &ops.WriteAtomicOp{
			Op:      ops.OpFileWriteAtomic,
			Path:    "conflict.go",
			Content: "package bar\n",
		},
	}

	_, err := ApplyAnyOps(root, []ops.AnyOp{replaceOp, writeOp}, ApplyOptions{DryRun: false})
	if err == nil {
		t.Fatal("expected error for conflicting ops on same path")
	}
}

// ---- ApplyAnyOps: file.mkdir_all ----

func TestApplyAnyOps_MkdirAll_CreatesDir(t *testing.T) {
	root := t.TempDir()

	anyOp := ops.AnyOp{
		Op:   ops.OpFileMkdirAll,
		Path: "pkg/sub",
		MkdirAll: &ops.MkdirAllOp{
			Op:   ops.OpFileMkdirAll,
			Path: "pkg/sub",
		},
	}

	_, err := ApplyAnyOps(root, []ops.AnyOp{anyOp}, ApplyOptions{DryRun: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	st, err := os.Stat(filepath.Join(root, "pkg", "sub"))
	if err != nil || !st.IsDir() {
		t.Fatalf("expected directory to be created: %v", err)
	}
}

func TestApplyAnyOps_MkdirAll_DryRunSkipsCreate(t *testing.T) {
	root := t.TempDir()

	anyOp := ops.AnyOp{
		Op:   ops.OpFileMkdirAll,
		Path: "drydir",
		MkdirAll: &ops.MkdirAllOp{
			Op:   ops.OpFileMkdirAll,
			Path: "drydir",
		},
	}

	_, err := ApplyAnyOps(root, []ops.AnyOp{anyOp}, ApplyOptions{DryRun: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "drydir")); !os.IsNotExist(statErr) {
		t.Fatal("directory should not be created in dry-run")
	}
}

// ---- Multiple replace_range on same file ----

func TestApplyAnyOps_MultipleReplaceRange_SameFile(t *testing.T) {
	root := t.TempDir()
	f := filepath.Join(root, "multi.go")
	content := "line0\nline1\nline2\nline3\n"
	if err := os.WriteFile(f, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Two ops on different lines — must be applied bottom-to-top.
	op1 := ops.AnyOp{
		Op:   ops.OpFileReplaceRange,
		Path: "multi.go",
		ReplaceRange: &ops.ReplaceRangeOp{
			Op:          ops.OpFileReplaceRange,
			Path:        "multi.go",
			Range:       ops.Range{Start: ops.Position{Line: 1, Col: 0}, End: ops.Position{Line: 1, Col: 5}},
			Expected:    "line1",
			Replacement: "LINE1",
		},
	}
	op2 := ops.AnyOp{
		Op:   ops.OpFileReplaceRange,
		Path: "multi.go",
		ReplaceRange: &ops.ReplaceRangeOp{
			Op:          ops.OpFileReplaceRange,
			Path:        "multi.go",
			Range:       ops.Range{Start: ops.Position{Line: 3, Col: 0}, End: ops.Position{Line: 3, Col: 5}},
			Expected:    "line3",
			Replacement: "LINE3",
		},
	}

	result, err := ApplyAnyOps(root, []ops.AnyOp{op1, op2}, ApplyOptions{DryRun: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ChangedFiles) != 1 {
		t.Fatalf("expected 1 changed file, got %d", len(result.ChangedFiles))
	}

	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := "line0\nLINE1\nline2\nLINE3\n"
	if string(b) != want {
		t.Errorf("content:\ngot  %q\nwant %q", string(b), want)
	}
}

// ---- Backup on successful write ----

func TestApplyOps_Backup_CreatedOnSuccess(t *testing.T) {
	root := t.TempDir()
	f := filepath.Join(root, "src.go")
	original := "package main\n\nfunc old() {}\n"
	if err := os.WriteFile(f, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	h := cache.ComputeSHA256([]byte(original))

	op := ops.ReplaceRangeOp{
		Op:          ops.OpFileReplaceRange,
		Path:        "src.go",
		Range:       ops.Range{Start: ops.Position{Line: 2, Col: 0}, End: ops.Position{Line: 2, Col: len("func old() {}")}},
		Expected:    "func old() {}",
		Replacement: "func new() {}",
		Conditions:  ops.Conditions{FileHash: h},
	}

	_, err := ApplyOps(root, []ops.ReplaceRangeOp{op}, ApplyOptions{DryRun: false, Backup: true, BackupSuffix: ".bak"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	bak, err := os.ReadFile(f + ".bak")
	if err != nil {
		t.Fatalf("backup not created: %v", err)
	}
	if string(bak) != original {
		t.Errorf("backup content mismatch: %q", string(bak))
	}

	cur, _ := os.ReadFile(f)
	if string(cur) == original {
		t.Error("file was not modified after successful apply")
	}
}
