package sessionfile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDelete_RemovesTheTrajectorySidecar(t *testing.T) {
	root := t.TempDir()
	if err := Save(root, &Snapshot{ID: "s1"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	sidecar := filepath.Join(root, ".orchestra", "sessions", "s1.events.jsonl")
	if err := os.WriteFile(sidecar, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}

	if err := Delete(root, "s1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
		t.Errorf("sidecar still present after Delete (err = %v)", err)
	}
}

func TestDelete_KeepsTheSnapshotWhenTheSidecarCannotBeRemoved(t *testing.T) {
	// Delete must not destroy the primary record and then fail. With the
	// sidecar held open, Delete returns an error AND the snapshot survives, so
	// the session is still listed and the delete can be retried.
	if runtime.GOOS != "windows" {
		t.Skip("POSIX unlinks open files; this failure mode is Windows-only")
	}

	root := t.TempDir()
	if err := Save(root, &Snapshot{ID: "s1"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	snapPath := snapshotPath(root, "s1")
	sidecar := filepath.Join(root, ".orchestra", "sessions", "s1.events.jsonl")
	if err := os.WriteFile(sidecar, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}

	held, err := os.OpenFile(sidecar, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open sidecar to hold it: %v", err)
	}
	defer func() { _ = held.Close() }()

	if err := Delete(root, "s1"); err == nil {
		t.Fatal("expected Delete to fail while the sidecar is held open")
	}
	if _, err := os.Stat(snapPath); err != nil {
		t.Fatalf("expected snapshot to survive a failed Delete, stat: %v", err)
	}
}
