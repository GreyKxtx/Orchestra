package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveInWorkspace_Escape(t *testing.T) {
	root := t.TempDir()
	_, _, err := ResolveInWorkspace(root, "../outside")
	if err == nil {
		t.Fatal("expected escape error")
	}
}

func TestResolveInWorkspace_RelativeOK(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	abs, rel, err := ResolveInWorkspace(root, "a/b")
	if err != nil {
		t.Fatal(err)
	}
	if rel != "a/b" {
		t.Fatalf("rel=%q", rel)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatal(err)
	}
}
