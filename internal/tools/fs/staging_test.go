package fs_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/patch/cache"
	"github.com/orchestra/orchestra/patch/patches"
)

func newDryRunRunner(t *testing.T) *tools.Runner {
	t.Helper()
	root := t.TempDir()
	r, err := tools.NewRunner(root, tools.RunnerOptions{DryRun: true})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func TestStaging_NoFileCreatedOnDisk(t *testing.T) {
	r := newDryRunRunner(t)
	p := filepath.Join(r.WorkspaceRoot(), "hello.txt")
	if err := os.WriteFile(p, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	origHash := cache.ComputeSHA256([]byte("original"))

	_, err := r.FSWrite(context.Background(), tools.FSWriteRequest{
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

	_, err := r.FSWrite(context.Background(), tools.FSWriteRequest{
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
	p := filepath.Join(r.WorkspaceRoot(), "foo.txt")
	if err := os.WriteFile(p, []byte("before"), 0644); err != nil {
		t.Fatal(err)
	}
	origHash := cache.ComputeSHA256([]byte("before"))

	_, err := r.FSWrite(context.Background(), tools.FSWriteRequest{
		Path:     "foo.txt",
		Content:  "after",
		FileHash: origHash,
	})
	if err != nil {
		t.Fatalf("FSWrite: %v", err)
	}

	resp, err := r.FSRead(context.Background(), tools.FSReadRequest{Path: "foo.txt"})
	if err != nil {
		t.Fatalf("FSRead: %v", err)
	}
	if !strings.Contains(resp.Content, "after") {
		t.Fatalf("FSRead returned disk content instead of staged: %q", resp.Content)
	}
}

func TestStaging_EditDryRun(t *testing.T) {
	r := newDryRunRunner(t)
	p := filepath.Join(r.WorkspaceRoot(), "code.go")
	if err := os.WriteFile(p, []byte("package main\n\nfunc hello() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := r.FSEdit(context.Background(), tools.FSEditRequest{
		Path:    "code.go",
		Search:  "hello",
		Replace: "world",
	})
	if err != nil {
		t.Fatalf("FSEdit dry-run: %v", err)
	}

	got, _ := os.ReadFile(p)
	if string(got) != "package main\n\nfunc hello() {}\n" {
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
	p := filepath.Join(r.WorkspaceRoot(), "src.go")
	if err := os.WriteFile(p, []byte("package main\n\nfunc hello() {}\n"), 0644); err != nil {
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

	staged := r.StagedFileContent()
	content, ok := staged["src.go"]
	if !ok {
		t.Fatal("expected file to be staged")
	}
	if !strings.Contains(content, "world") || strings.Contains(content, "hello") {
		t.Fatalf("unexpected staged content: %q", content)
	}
	if got, err := os.ReadFile(p); err != nil || string(got) != "package main\n\nfunc hello() {}\n" {
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

	staged := r.StagedFileContent()
	content, ok := staged["newfile.txt"]
	if !ok {
		t.Fatal("expected file to be staged")
	}
	if content != "brand new content" {
		t.Fatalf("unexpected content: %q", content)
	}
	if _, err := os.Stat(filepath.Join(r.WorkspaceRoot(), "newfile.txt")); err == nil {
		t.Fatal("file should not exist on disk in dry-run")
	}
}

func TestStaging_ApplyPatchesToStaged_StaleFileHash(t *testing.T) {
	r := newDryRunRunner(t)
	p := filepath.Join(r.WorkspaceRoot(), "file.txt")
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

func TestFSWrite_MustNotExist_AllowsRestageInDryRun(t *testing.T) {
	r := newDryRunRunner(t)

	_, err := r.FSWrite(context.Background(), tools.FSWriteRequest{
		Path:         "new.txt",
		Content:      "v1",
		MustNotExist: true,
	})
	if err != nil {
		t.Fatalf("first write: %v", err)
	}

	_, err = r.FSWrite(context.Background(), tools.FSWriteRequest{
		Path:         "new.txt",
		Content:      "v2",
		MustNotExist: true,
	})
	if err != nil {
		t.Fatalf("re-stage with must_not_exist should succeed in dry-run: %v", err)
	}

	resp, err := r.FSRead(context.Background(), tools.FSReadRequest{Path: "new.txt"})
	if err != nil {
		t.Fatalf("FSRead: %v", err)
	}
	if !strings.Contains(resp.Content, "v2") {
		t.Fatalf("expected staged v2, got %q", resp.Content)
	}
}

func TestStaging_MergeIntoList_ShowsStagedOnlyFiles(t *testing.T) {
	r := newDryRunRunner(t)
	_, err := r.FSWrite(context.Background(), tools.FSWriteRequest{
		Path:         "pkg/new.go",
		Content:      "package pkg\n",
		MustNotExist: true,
	})
	if err != nil {
		t.Fatalf("FSWrite: %v", err)
	}

	resp, err := r.FSList(context.Background(), tools.FSListRequest{Path: ".", Recursive: boolPtr(true)})
	if err != nil {
		t.Fatalf("FSList: %v", err)
	}
	found := false
	for _, f := range resp.Files {
		if f.Path == "pkg/new.go" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected staged file in ls output, got %v", resp.Files)
	}
}

func TestStaging_ASTGate_RejectsInvalidGo(t *testing.T) {
	r := newDryRunRunner(t)

	_, err := r.FSWrite(context.Background(), tools.FSWriteRequest{
		Path:         "broken.go",
		Content:      "package main\n\nfunc main() {\n",
		MustNotExist: true,
	})
	if err == nil {
		t.Fatal("expected SyntaxError")
	}
	if !strings.Contains(err.Error(), "SyntaxError") {
		t.Fatalf("expected SyntaxError, got %v", err)
	}
	if len(r.StagedOps()) != 0 {
		t.Fatal("invalid content must not reach staging overlay")
	}
}

func TestStaging_ASTGate_CanDisable(t *testing.T) {
	root := t.TempDir()
	r, err := tools.NewRunner(root, tools.RunnerOptions{DryRun: true, DisableASTGate: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })

	_, err = r.FSWrite(context.Background(), tools.FSWriteRequest{
		Path:         "broken.go",
		Content:      "package main\n\nfunc main() {\n",
		MustNotExist: true,
	})
	if err != nil {
		t.Fatalf("gate disabled: %v", err)
	}
	if len(r.StagedOps()) != 1 {
		t.Fatal("expected staged op with gate off")
	}
}

func boolPtr(v bool) *bool { return &v }

func TestStaging_ClearStaged(t *testing.T) {
	r := newDryRunRunner(t)
	_, _ = r.FSWrite(context.Background(), tools.FSWriteRequest{Path: "x.txt", Content: "hi", MustNotExist: true})
	if len(r.StagedOps()) == 0 {
		t.Fatal("expected staged ops before clear")
	}
	r.ClearStaged()
	if len(r.StagedOps()) != 0 {
		t.Fatal("expected empty staged ops after clear")
	}
}

func TestCommitStagedPath_WritesToDisk(t *testing.T) {
	r := newDryRunRunner(t)
	_, err := r.FSWrite(context.Background(), tools.FSWriteRequest{
		Path:    "live.txt",
		Content: "on disk now",
	})
	if err != nil {
		t.Fatalf("FSWrite: %v", err)
	}
	resp, err := r.CommitStagedPath(context.Background(), "live.txt", false)
	if err != nil {
		t.Fatalf("CommitStagedPath: %v", err)
	}
	if resp == nil || !resp.Applied || len(resp.ChangedFiles) != 1 {
		t.Fatalf("commit response: %+v", resp)
	}
	b, err := os.ReadFile(filepath.Join(r.WorkspaceRoot(), "live.txt"))
	if err != nil {
		t.Fatalf("read disk: %v", err)
	}
	if string(b) != "on disk now" {
		t.Fatalf("disk content: %q", b)
	}
	if len(r.StagedOps()) != 0 {
		t.Fatal("expected overlay cleared after commit")
	}
}
