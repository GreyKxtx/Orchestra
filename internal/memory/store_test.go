package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStore_FormatInject_Tiered(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ORCHESTRA.md"), "ORCH RULES")
	memDir := filepath.Join(dir, ".orchestra", "memory")
	writeFile(t, filepath.Join(memDir, "agent.md"), "---\n*2026-01-01T00:00:00Z*\n\nold\n---\n*2026-01-02T00:00:00Z*\n\nnewest\n")

	cfg := DefaultConfig()
	cfg.Mode = ModeEager
	store := NewStore(dir, "", cfg)
	block := store.FormatInject(4096)
	if !strings.Contains(block, "ORCH RULES") {
		t.Fatal("missing orchestra")
	}
	if !strings.Contains(block, "newest") {
		t.Fatal("missing recent agent entry")
	}
	if !strings.HasPrefix(block, "<project_memory>") {
		t.Fatal("expected wrapper")
	}
}

func TestStore_SessionMemory(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	store := NewStore(dir, "sess-1", cfg)

	rel, n, err := store.Append("session", "remember foo")
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 || rel == "" {
		t.Fatal("expected write")
	}
	block := store.FormatInject(8192)
	if !strings.Contains(block, "remember foo") {
		t.Fatalf("session memory not injected: %s", block)
	}
}

func TestStore_MemoryRead_List(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ORCHESTRA.md"), "hello")
	cfg := DefaultConfig()
	store := NewStore(dir, "", cfg)
	res := store.Read("", "", 0)
	if len(res.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(res.Entries))
	}
}

func TestStore_MemoryRead_LessonsLayer(t *testing.T) {
	dir := t.TempDir()
	lessonsDir := filepath.Join(dir, ".orchestra", "memory", "lessons")
	writeFile(t, filepath.Join(lessonsDir, "engineering.md"), "## lesson\n- task: smoke\n")
	cfg := DefaultConfig()
	store := NewStore(dir, "", cfg)
	res := store.Read("lessons", "", 4096)
	if !strings.Contains(res.Content, "smoke") {
		t.Fatalf("lessons layer: %+v", res)
	}
	if res.Layer != layerLessons {
		t.Fatalf("layer=%q", res.Layer)
	}
	list := store.Read("", "", 0)
	found := false
	for _, e := range list.Entries {
		if e.Layer == layerLessons {
			found = true
		}
	}
	if !found {
		t.Fatalf("list entries=%+v", list.Entries)
	}
}

func TestStore_CompactAgentFile(t *testing.T) {
	dir := t.TempDir()
	memDir := filepath.Join(dir, ".orchestra", "memory")
	if err := os.MkdirAll(memDir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(memDir, "agent.md")
	var b strings.Builder
	for i := 0; i < 50; i++ {
		b.WriteString("\n---\n*2026-01-01T00:00:00Z*\n\n")
		b.WriteString(strings.Repeat("x", 500))
	}
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.MaxAgentKB = 4
	store := NewStore(dir, "", cfg)
	if err := store.compactAgentFile(path); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Size() > int64(cfg.MaxAgentBytes()+512) {
		t.Fatalf("file still too large: %d", info.Size())
	}
}

func TestStore_LazyOrchestra(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "pkg")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "ORCHESTRA.md"), "lazy rules")
	cfg := DefaultConfig()
	store := NewStore(dir, "", cfg)
	got := store.LazyOrchestra(sub)
	if got != "lazy rules" {
		t.Fatalf("got %q", got)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
