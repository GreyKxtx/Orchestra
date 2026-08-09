package sessionfile_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/internal/sessionfile"
)

func TestParseSnapshot_V2RoundTrip(t *testing.T) {
	dir := t.TempDir()
	snap := &sessionfile.Snapshot{
		Version:   sessionfile.Version,
		ID:        "20260805T120000-abcd",
		Title:     "hello",
		Model:     "m1",
		CreatedAt: time.Now().UTC().Add(-time.Hour),
		History:   []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		UIMessages: []sessionfile.UIMessage{
			{Role: "user", Text: "hi"},
		},
	}
	if err := sessionfile.Save(dir, snap); err != nil {
		t.Fatal(err)
	}
	loaded, err := sessionfile.Load(dir, snap.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Version != sessionfile.Version {
		t.Fatalf("version=%d", loaded.Version)
	}
	if len(loaded.History) != 1 || len(loaded.UIMessages) != 1 {
		t.Fatalf("history=%d ui=%d", len(loaded.History), len(loaded.UIMessages))
	}
}

func TestParseSnapshot_MigrateV1(t *testing.T) {
	v1 := map[string]any{
		"version":       1,
		"id":            "abc123",
		"history":       []map[string]any{{"role": "user", "content": "x"}},
		"created_at":    time.Now().UTC().Format(time.RFC3339Nano),
		"last_activity": time.Now().UTC().Format(time.RFC3339Nano),
	}
	data, _ := json.Marshal(v1)
	snap, err := sessionfile.ParseSnapshot(data, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Version != sessionfile.Version {
		t.Fatalf("version=%d", snap.Version)
	}
	if len(snap.History) != 1 {
		t.Fatalf("history=%d", len(snap.History))
	}
	if snap.UIMessages == nil {
		t.Fatal("expected empty ui_messages slice")
	}
}

func TestParseSnapshot_MigrateV0(t *testing.T) {
	v0 := map[string]any{
		"id":         "sess1",
		"title":      "My chat",
		"created_at": time.Now().UTC().Format(time.RFC3339Nano),
		"updated_at": time.Now().UTC().Format(time.RFC3339Nano),
		"messages": []map[string]any{
			{"Role": "user", "Text": "hello"},
		},
	}
	data, _ := json.Marshal(v0)
	snap, err := sessionfile.ParseSnapshot(data, "sess1")
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.UIMessages) != 1 || snap.UIMessages[0].Text != "hello" {
		t.Fatalf("ui=%+v", snap.UIMessages)
	}
	if len(snap.History) != 0 {
		t.Fatalf("expected empty history, got %d", len(snap.History))
	}
}

func TestListMeta_ReadsMigratedFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".orchestra", "sessions", "legacy.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	v0 := []byte(`{"id":"legacy","title":"t","messages":[{"Role":"user","Text":"x"}]}`)
	if err := os.WriteFile(path, v0, 0o600); err != nil {
		t.Fatal(err)
	}
	metas, err := sessionfile.ListMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 1 || metas[0].ID != "legacy" {
		t.Fatalf("metas=%+v", metas)
	}
}
