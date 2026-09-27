package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// The core's runner follows the tree with file notifications so the code
// graph refreshes from what changed (phase 7); a runner built without
// WatchCKG, and one built with ORCHESTRA_CKG_WATCH=0, walk.

func TestRunner_FollowsTheTreeWhenAsked(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := NewRunner(root, RunnerOptions{WatchCKG: true})
	if err != nil {
		t.Fatal(err)
	}
	view, err := r.CKGIndexStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !view.Watching {
		t.Fatal("a runner asked to watch reports it does not")
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.CKGIndexStatus(context.Background()); err != nil {
		t.Fatalf("index status after Close: %v", err)
	}
}

func TestRunner_WalksWithoutAWatcher(t *testing.T) {
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	view, err := r.CKGIndexStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if view.Watching {
		t.Fatal("a runner nobody asked to watch reports it does")
	}
}

func TestRunner_WatcherCanBeTurnedOffByEnv(t *testing.T) {
	t.Setenv("ORCHESTRA_CKG_WATCH", "0")
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{WatchCKG: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	view, err := r.CKGIndexStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if view.Watching {
		t.Fatal("ORCHESTRA_CKG_WATCH=0 did not turn the watcher off")
	}
}
