package sessionfile

import (
	"os"
	"path/filepath"
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
