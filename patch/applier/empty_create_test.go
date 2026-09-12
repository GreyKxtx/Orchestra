package applier

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/patch/ops"
)

// Creating a file with no content used to be a silent no-op.
//
// The apply loop decided what to write by comparing the planned bytes with
// the bytes already there, and for a file that does not exist yet both sides
// are empty. The plan was skipped, the call returned success, and the caller
// was told bytes_written 0 — which is true and reads as fine. The file was
// never created, so the next read of that path answered "no such path in the
// workspace", and nothing in between had reported a failure.

func emptyCreateOp(path string) ops.AnyOp {
	wa := ops.WriteAtomicOp{
		Op:         ops.OpFileWriteAtomic,
		Path:       path,
		Content:    "",
		Conditions: ops.WriteAtomicConditions{MustNotExist: true},
	}
	return ops.AnyOp{Op: wa.Op, Path: wa.Path, WriteAtomic: &wa}
}

func TestApply_CreatesAFileThatHasNoContent(t *testing.T) {
	root := t.TempDir()

	res, err := ApplyAnyOps(root, []ops.AnyOp{emptyCreateOp("empty.txt")}, ApplyOptions{DryRun: false})
	if err != nil {
		t.Fatalf("ApplyAnyOps: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "empty.txt")); statErr != nil {
		t.Fatalf("the op reported success and created nothing: %v", statErr)
	}
	// The caller has to be able to tell that something happened, or a dry-run
	// preview of this op shows no change at all.
	if len(res.ChangedFiles) != 1 || res.ChangedFiles[0] != "empty.txt" {
		t.Errorf("creating a file must count as a change, got ChangedFiles=%v", res.ChangedFiles)
	}
}

// The other half of the rule: writing bytes a file already has stays a no-op,
// so an unchanged file is not rewritten, not backed up, and not reported.
func TestApply_RewritingIdenticalContentStaysANoOp(t *testing.T) {
	root := t.TempDir()
	const body = "package main\n\nfunc main() {}\n"
	abs := filepath.Join(root, "main.go")
	if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(abs)
	if err != nil {
		t.Fatal(err)
	}

	wa := ops.WriteAtomicOp{
		Op:      ops.OpFileWriteAtomic,
		Path:    "main.go",
		Content: body,
	}
	res, err := ApplyAnyOps(root, []ops.AnyOp{{Op: wa.Op, Path: wa.Path, WriteAtomic: &wa}},
		ApplyOptions{DryRun: false, Backup: true, BackupSuffix: ".orchestra.bak"})
	if err != nil {
		t.Fatalf("ApplyAnyOps: %v", err)
	}
	if len(res.ChangedFiles) != 0 {
		t.Errorf("rewriting the same bytes is not a change, got %v", res.ChangedFiles)
	}
	if _, statErr := os.Stat(abs + ".orchestra.bak"); statErr == nil {
		t.Error("an unchanged file must not be backed up")
	}
	after, err := os.Stat(abs)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("an unchanged file must not be rewritten")
	}
}
