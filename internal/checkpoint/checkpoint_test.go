package checkpoint

import (
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/orchestra/orchestra/llm"
)

func TestSaveLoadLatest(t *testing.T) {
	root := t.TempDir()
	if _, err := Latest(root); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no runs yet: %v", err)
	}
	old := &Checkpoint{RunID: "20260925T100000-aaaa", ProjectRoot: root, Status: StatusDone, Params: json.RawMessage(`{"query":"old"}`)}
	mid := &Checkpoint{RunID: "20260925T110000-bbbb", ProjectRoot: root, Status: StatusRunning, Params: json.RawMessage(`{"query":"mid"}`),
		History: []llm.Message{{Role: llm.RoleAssistant, Content: "step one"}},
		Staged:  []StagedFile{{Path: "a.go", Content: "package a\n", DiskHash: "h"}}}
	newest := &Checkpoint{RunID: "20260925T120000-cccc", ProjectRoot: root, Status: StatusDone, Params: json.RawMessage(`{}`)}
	for _, c := range []*Checkpoint{old, mid, newest} {
		if err := Save(root, c); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Latest(root)
	if err != nil || got.RunID != mid.RunID {
		t.Fatalf("the latest resumable run is the running one, not the finished newer one: %+v %v", got, err)
	}
	if len(got.History) != 1 || got.Staged[0].DiskHash != "h" || string(got.Params) != `{"query":"mid"}` {
		t.Fatalf("round trip: %+v", got)
	}
	if _, err := Load(root, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown run: %v", err)
	}
	if _, err := Load(root, "../escape"); err == nil {
		t.Fatal("a run id cannot name a path outside the runs directory")
	}
}

func TestLoad_RefusesAnotherVersion(t *testing.T) {
	root := t.TempDir()
	c := &Checkpoint{RunID: "20260925T100000-aaaa", Status: StatusRunning, Params: json.RawMessage(`{}`)}
	if err := Save(root, c); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(Path(root, c.RunID))
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	m["version"] = Version + 1
	data, _ = json.Marshal(m)
	if err := os.WriteFile(Path(root, c.RunID), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, c.RunID); err == nil {
		t.Fatal("a checkpoint of another format version is not read as this one")
	}
}
