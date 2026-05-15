package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func newFSExtraRunner(t *testing.T) (*Runner, string) {
	t.Helper()
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r, root
}

func TestFSDelete_File(t *testing.T) {
	r, root := newFSExtraRunner(t)

	path := filepath.Join(root, "todelete.txt")
	if err := os.WriteFile(path, []byte("bye"), 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := r.FSDelete(context.Background(), FSDeleteRequest{Path: "todelete.txt"})
	if err != nil {
		t.Fatalf("FSDelete: %v", err)
	}
	if resp.Path != "todelete.txt" {
		t.Errorf("path=%q, want %q", resp.Path, "todelete.txt")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Error("file still exists after delete")
	}
}

func TestFSDelete_Dir_Recursive(t *testing.T) {
	r, root := newFSExtraRunner(t)

	dir := filepath.Join(root, "subdir")
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := r.FSDelete(context.Background(), FSDeleteRequest{Path: "subdir", Recursive: true})
	if err != nil {
		t.Fatalf("FSDelete recursive: %v", err)
	}
	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Error("dir still exists after recursive delete")
	}
}

func TestFSDelete_NonRecursive_NonEmpty_Fails(t *testing.T) {
	r, root := newFSExtraRunner(t)

	dir := filepath.Join(root, "nonempty")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := r.FSDelete(context.Background(), FSDeleteRequest{Path: "nonempty", Recursive: false})
	if err == nil {
		t.Fatal("expected error deleting non-empty dir without recursive=true")
	}
}

func TestFSDelete_PathTraversal(t *testing.T) {
	r, _ := newFSExtraRunner(t)
	_, err := r.FSDelete(context.Background(), FSDeleteRequest{Path: "../outside.txt"})
	if err == nil {
		t.Fatal("expected path traversal error")
	}
}

func TestFSDelete_NotExist(t *testing.T) {
	r, _ := newFSExtraRunner(t)
	_, err := r.FSDelete(context.Background(), FSDeleteRequest{Path: "nope.txt"})
	if err == nil {
		t.Fatal("expected error for non-existent path")
	}
}

func TestFSDelete_EmptyPath(t *testing.T) {
	r, _ := newFSExtraRunner(t)
	_, err := r.FSDelete(context.Background(), FSDeleteRequest{Path: ""})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestFSRename_File(t *testing.T) {
	r, root := newFSExtraRunner(t)

	if err := os.WriteFile(filepath.Join(root, "before.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := r.FSRename(context.Background(), FSRenameRequest{Path: "before.txt", NewPath: "after.txt"})
	if err != nil {
		t.Fatalf("FSRename: %v", err)
	}
	if resp.Path != "before.txt" || resp.NewPath != "after.txt" {
		t.Errorf("unexpected resp: %+v", resp)
	}
	if _, statErr := os.Stat(filepath.Join(root, "before.txt")); !os.IsNotExist(statErr) {
		t.Error("old path still exists")
	}
	if _, statErr := os.Stat(filepath.Join(root, "after.txt")); statErr != nil {
		t.Errorf("new path doesn't exist: %v", statErr)
	}
}

func TestFSRename_CreatesParentDirs(t *testing.T) {
	r, root := newFSExtraRunner(t)

	if err := os.WriteFile(filepath.Join(root, "src.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := r.FSRename(context.Background(), FSRenameRequest{Path: "src.txt", NewPath: "newdir/dst.txt"})
	if err != nil {
		t.Fatalf("FSRename: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "newdir", "dst.txt")); statErr != nil {
		t.Errorf("new path doesn't exist: %v", statErr)
	}
}

func TestFSRename_PathTraversal_Src(t *testing.T) {
	r, _ := newFSExtraRunner(t)
	_, err := r.FSRename(context.Background(), FSRenameRequest{Path: "../out.txt", NewPath: "dst.txt"})
	if err == nil {
		t.Fatal("expected path traversal error on src")
	}
}

func TestFSRename_PathTraversal_Dst(t *testing.T) {
	r, root := newFSExtraRunner(t)
	if err := os.WriteFile(filepath.Join(root, "src.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := r.FSRename(context.Background(), FSRenameRequest{Path: "src.txt", NewPath: "../escape.txt"})
	if err == nil {
		t.Fatal("expected path traversal error on dst")
	}
}

func TestFSRename_SrcNotExist(t *testing.T) {
	r, _ := newFSExtraRunner(t)
	_, err := r.FSRename(context.Background(), FSRenameRequest{Path: "nope.txt", NewPath: "out.txt"})
	if err == nil {
		t.Fatal("expected error when src does not exist")
	}
}

func TestFSRename_SrcEqualsDst(t *testing.T) {
	r, root := newFSExtraRunner(t)
	if err := os.WriteFile(filepath.Join(root, "same.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := r.FSRename(context.Background(), FSRenameRequest{Path: "same.txt", NewPath: "same.txt"})
	if err == nil {
		t.Fatal("expected error when src == dst")
	}
}

func TestFSRename_DstExists(t *testing.T) {
	r, root := newFSExtraRunner(t)
	if err := os.WriteFile(filepath.Join(root, "src.txt"), []byte("src"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dst.txt"), []byte("dst"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := r.FSRename(context.Background(), FSRenameRequest{Path: "src.txt", NewPath: "dst.txt"})
	if err == nil {
		t.Fatal("expected error when destination already exists")
	}
}

func TestFSRename_DryRun(t *testing.T) {
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{DryRun: true})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()
	if err := os.WriteFile(filepath.Join(root, "orig.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	resp, err := r.FSRename(context.Background(), FSRenameRequest{Path: "orig.txt", NewPath: "moved.txt"})
	if err != nil {
		t.Fatalf("FSRename dry-run: %v", err)
	}
	if resp.Path != "orig.txt" || resp.NewPath != "moved.txt" {
		t.Errorf("unexpected resp: %+v", resp)
	}
	// File should still exist (dry-run)
	if _, statErr := os.Stat(filepath.Join(root, "orig.txt")); statErr != nil {
		t.Error("orig.txt should still exist in dry-run mode")
	}
}
