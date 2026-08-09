package fs_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/patch/cache"
)

func newReadRunner(t *testing.T) (*tools.Runner, string) {
	t.Helper()
	root := t.TempDir()
	r, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r, root
}

func TestFSReadLineNumbers(t *testing.T) {
	r, root := newReadRunner(t)
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := r.FSRead(context.Background(), tools.FSReadRequest{Path: "f.txt"})
	if err != nil {
		t.Fatalf("FSRead: %v", err)
	}
	want := "1: alpha\n2: beta\n"
	if resp.Content != want {
		t.Errorf("Content:\ngot:  %q\nwant: %q", resp.Content, want)
	}
}

func TestFSReadHashUnchanged(t *testing.T) {
	r, root := newReadRunner(t)
	raw := []byte("line one\nline two\n")
	if err := os.WriteFile(filepath.Join(root, "h.txt"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := r.FSRead(context.Background(), tools.FSReadRequest{Path: "h.txt"})
	if err != nil {
		t.Fatalf("FSRead: %v", err)
	}

	wantHash := cache.ComputeSHA256(raw)
	if resp.SHA256 != wantHash {
		t.Errorf("SHA256 mismatch:\ngot:  %s\nwant: %s", resp.SHA256, wantHash)
	}
	if resp.FileHash != wantHash {
		t.Errorf("FileHash mismatch:\ngot:  %s\nwant: %s", resp.FileHash, wantHash)
	}
}
