package sessionfile

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/orchestra/orchestra/llm"
)

func TestExportImport_Roundtrip(t *testing.T) {
	root := t.TempDir()
	id := "20260810T120000-abcd"
	snap := &Snapshot{
		ID:        id,
		Title:     "test session",
		Model:     "qwen-27b",
		CreatedAt: time.Now().UTC(),
		History:   []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
		UIMessages: []UIMessage{
			{Role: "user", Text: "hello"},
		},
	}
	if err := Save(root, snap); err != nil {
		t.Fatalf("save: %v", err)
	}

	data, err := Export(root, id)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	destRoot := t.TempDir()
	gotID, err := Import(destRoot, data, ImportOptions{})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if gotID != id {
		t.Fatalf("id: want %q got %q", id, gotID)
	}

	loaded, err := Load(destRoot, id)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Title != snap.Title {
		t.Fatalf("title: want %q got %q", snap.Title, loaded.Title)
	}
	if len(loaded.History) != 1 || loaded.History[0].Content != "hello" {
		t.Fatalf("history: %+v", loaded.History)
	}
}

func TestImport_RawSnapshotFile(t *testing.T) {
	root := t.TempDir()
	id := "20260810T120001-ef01"
	snap := &Snapshot{
		ID:    id,
		Title: "raw",
		UIMessages: []UIMessage{
			{Role: "user", Text: "hi"},
		},
	}
	if err := Save(root, snap); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".orchestra", "sessions", id+".json"))
	if err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	gotID, err := Import(dest, raw, ImportOptions{})
	if err != nil {
		t.Fatalf("import raw: %v", err)
	}
	if gotID != id {
		t.Fatalf("id mismatch")
	}
}

func TestImport_ConflictRequiresForce(t *testing.T) {
	root := t.TempDir()
	id := "20260810T120002-1111"
	snap := &Snapshot{ID: id, UIMessages: []UIMessage{{Role: "user", Text: "x"}}}
	if err := Save(root, snap); err != nil {
		t.Fatal(err)
	}
	data, err := Export(root, id)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Import(root, data, ImportOptions{}); err == nil {
		t.Fatal("expected conflict error")
	}
	if _, err := Import(root, data, ImportOptions{Force: true}); err != nil {
		t.Fatalf("force import: %v", err)
	}
}

func TestValidateSessionID_RejectsTraversal(t *testing.T) {
	for _, bad := range []string{"../x", "a/b", "", "foo\\bar"} {
		if err := ValidateSessionID(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}
