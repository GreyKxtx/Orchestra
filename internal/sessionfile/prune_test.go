package sessionfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func saveWithAge(t *testing.T, root, id string, age time.Duration) {
	t.Helper()
	if err := Save(root, &Snapshot{ID: id, Title: id}); err != nil {
		t.Fatal(err)
	}
	// Save stamps UpdatedAt with now; rewrite the file with the wanted age.
	snap, err := Load(root, id)
	if err != nil {
		t.Fatal(err)
	}
	snap.UpdatedAt = time.Now().Add(-age)
	data, _ := json.MarshalIndent(snap, "", "  ")
	if err := os.WriteFile(snapshotPath(root, id), data, 0o600); err != nil {
		t.Fatal(err)
	}
	// A sidecar, so the test sees it go with the session.
	if err := os.WriteFile(filepath.Join(sessionsDir(root), id+".events.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Prune keeps the newest keep sessions and everything a core still holds,
// and removes what is older than maxAge; sidecars go with their session.
func TestPrune_KeepsTheNewestAndTheProtected(t *testing.T) {
	root := t.TempDir()
	saveWithAge(t, root, "oldest", 72*time.Hour)
	saveWithAge(t, root, "older", 48*time.Hour)
	saveWithAge(t, root, "held", 36*time.Hour)
	saveWithAge(t, root, "newest", time.Hour)

	removed, err := Prune(root, 2, 0, map[string]bool{"held": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 2 || removed[0] != "older" || removed[1] != "oldest" {
		t.Fatalf("removed %v, want [older oldest]", removed)
	}
	metas, _ := ListMeta(root)
	if len(metas) != 2 || metas[0].ID != "newest" || metas[1].ID != "held" {
		t.Fatalf("left %+v", metas)
	}
	if _, err := os.Stat(filepath.Join(sessionsDir(root), "older.events.jsonl")); !os.IsNotExist(err) {
		t.Fatal("the pruned session's sidecar survived")
	}
}

func TestPrune_ByAgeAndUnbounded(t *testing.T) {
	root := t.TempDir()
	saveWithAge(t, root, "old", 10*24*time.Hour)
	saveWithAge(t, root, "recent", time.Hour)
	if removed, err := Prune(root, -1, 0, nil); err != nil || len(removed) != 0 {
		t.Fatalf("unbounded prune removed %v, %v", removed, err)
	}
	removed, err := Prune(root, -1, 7*24*time.Hour, nil)
	if err != nil || len(removed) != 1 || removed[0] != "old" {
		t.Fatalf("removed %v, %v; want [old]", removed, err)
	}
}
