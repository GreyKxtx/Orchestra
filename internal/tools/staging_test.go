package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/cache"
	"github.com/orchestra/orchestra/internal/patches"
)

func newDryRunRunner(t *testing.T) *Runner {
	t.Helper()
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{DryRun: true})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func TestStaging_NoFileCreatedOnDisk(t *testing.T) {
	r := newDryRunRunner(t)
	p := filepath.Join(r.workspaceRoot, "hello.txt")
	if err := os.WriteFile(p, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	origHash := cache.ComputeSHA256([]byte("original"))

	_, err := r.FSWrite(nil, FSWriteRequest{
		Path:     "hello.txt",
		Content:  "staged content",
		FileHash: origHash,
	})
	if err != nil {
		t.Fatalf("FSWrite dry-run: %v", err)
	}

	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original" {
		t.Fatalf("disk was modified: got %q", string(got))
	}
}

func TestStaging_StagedOps_HasWriteAtomic(t *testing.T) {
	r := newDryRunRunner(t)

	_, err := r.FSWrite(nil, FSWriteRequest{
		Path:         "new.txt",
		Content:      "hello",
		MustNotExist: true,
	})
	if err != nil {
		t.Fatalf("FSWrite: %v", err)
	}

	ops := r.StagedOps()
	if len(ops) != 1 {
		t.Fatalf("expected 1 staged op, got %d", len(ops))
	}
	if ops[0].WriteAtomic == nil {
		t.Fatal("expected write_atomic op")
	}
	if ops[0].WriteAtomic.Content != "hello" {
		t.Fatalf("unexpected content: %q", ops[0].WriteAtomic.Content)
	}
	if !ops[0].WriteAtomic.Conditions.MustNotExist {
		t.Fatal("expected MustNotExist=true for new file")
	}
}

func TestStaging_ReadAfterWrite(t *testing.T) {
	r := newDryRunRunner(t)
	p := filepath.Join(r.workspaceRoot, "foo.txt")
	if err := os.WriteFile(p, []byte("before"), 0644); err != nil {
		t.Fatal(err)
	}
	origHash := cache.ComputeSHA256([]byte("before"))

	_, err := r.FSWrite(nil, FSWriteRequest{
		Path:     "foo.txt",
		Content:  "after",
		FileHash: origHash,
	})
	if err != nil {
		t.Fatalf("FSWrite: %v", err)
	}

	resp, err := r.FSRead(nil, FSReadRequest{Path: "foo.txt"})
	if err != nil {
		t.Fatalf("FSRead: %v", err)
	}
	if !strings.Contains(resp.Content, "after") {
		t.Fatalf("FSRead returned disk content instead of staged: %q", resp.Content)
	}
}

func TestStaging_EditDryRun(t *testing.T) {
	r := newDryRunRunner(t)
	p := filepath.Join(r.workspaceRoot, "code.go")
	if err := os.WriteFile(p, []byte("func hello() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := r.FSEdit(nil, FSEditRequest{
		Path:    "code.go",
		Search:  "hello",
		Replace: "world",
	})
	if err != nil {
		t.Fatalf("FSEdit dry-run: %v", err)
	}

	got, _ := os.ReadFile(p)
	if string(got) != "func hello() {}\n" {
		t.Fatalf("disk was modified: %q", string(got))
	}

	stagedOps := r.StagedOps()
	if len(stagedOps) != 1 {
		t.Fatalf("expected 1 staged op, got %d", len(stagedOps))
	}
	if stagedOps[0].WriteAtomic == nil || !strings.Contains(stagedOps[0].WriteAtomic.Content, "world") {
		t.Fatalf("staged content wrong: %+v", stagedOps[0].WriteAtomic)
	}
}

func TestStaging_ApplyPatchesToStaged_SearchReplace(t *testing.T) {
	r := newDryRunRunner(t)
	p := filepath.Join(r.workspaceRoot, "src.go")
	if err := os.WriteFile(p, []byte("func hello() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err := r.ApplyPatchesToStaged([]patches.Patch{{
		Type:    patches.TypeFileSearchReplace,
		Path:    "src.go",
		Search:  "hello",
		Replace: "world",
	}})
	if err != nil {
		t.Fatalf("ApplyPatchesToStaged: %v", err)
	}

	content, _, ok := r.stagedContent("src.go")
	if !ok {
		t.Fatal("expected file to be staged")
	}
	if !strings.Contains(content, "world") || strings.Contains(content, "hello") {
		t.Fatalf("unexpected staged content: %q", content)
	}
	if got, err := os.ReadFile(p); err != nil || string(got) != "func hello() {}\n" {
		t.Fatalf("disk was modified: %q", string(got))
	}
}

func TestStaging_ApplyPatchesToStaged_WriteAtomic(t *testing.T) {
	r := newDryRunRunner(t)

	err := r.ApplyPatchesToStaged([]patches.Patch{{
		Type:    patches.TypeFileWriteAtomic,
		Path:    "newfile.txt",
		Content: "brand new content",
	}})
	if err != nil {
		t.Fatalf("ApplyPatchesToStaged: %v", err)
	}

	content, _, ok := r.stagedContent("newfile.txt")
	if !ok {
		t.Fatal("expected file to be staged")
	}
	if content != "brand new content" {
		t.Fatalf("unexpected content: %q", content)
	}
	if _, err := os.Stat(filepath.Join(r.workspaceRoot, "newfile.txt")); err == nil {
		t.Fatal("file should not exist on disk in dry-run")
	}
}

func TestStaging_ApplyPatchesToStaged_StaleFileHash(t *testing.T) {
	r := newDryRunRunner(t)
	p := filepath.Join(r.workspaceRoot, "file.txt")
	if err := os.WriteFile(p, []byte("actual content"), 0644); err != nil {
		t.Fatal(err)
	}

	err := r.ApplyPatchesToStaged([]patches.Patch{{
		Type:     patches.TypeFileSearchReplace,
		Path:     "file.txt",
		Search:   "actual",
		Replace:  "modified",
		FileHash: "sha256:deadbeef000000000000000000000000000000000000000000000000deadbeef",
	}})
	if err == nil {
		t.Fatal("expected StaleContent error, got nil")
	}
	if !strings.Contains(err.Error(), "StaleContent") && !strings.Contains(err.Error(), "stale") && !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("expected stale error, got: %v", err)
	}
}

func TestStaging_ClearStaged(t *testing.T) {
	r := newDryRunRunner(t)
	_, _ = r.FSWrite(nil, FSWriteRequest{Path: "x.txt", Content: "hi", MustNotExist: true})
	if len(r.StagedOps()) == 0 {
		t.Fatal("expected staged ops before clear")
	}
	r.ClearStaged()
	if len(r.StagedOps()) != 0 {
		t.Fatal("expected empty staged ops after clear")
	}
}
