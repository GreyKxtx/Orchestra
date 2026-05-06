package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestComputeConfigHash_OrderIndependent(t *testing.T) {
	h1, err := ComputeConfigHash([]string{"b", "a"}, true, 64*1024, 1)
	if err != nil {
		t.Fatalf("ComputeConfigHash failed: %v", err)
	}
	h2, err := ComputeConfigHash([]string{"a", "b"}, true, 64*1024, 1)
	if err != nil {
		t.Fatalf("ComputeConfigHash failed: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("expected hashes to match, got %q and %q", h1, h2)
	}
}

func TestCache_SaveLoad_Roundtrip(t *testing.T) {
	tmp := t.TempDir()
	cachePath := filepath.Join(tmp, "cache.json")

	pid, err := ComputeProjectID(tmp)
	if err != nil {
		t.Fatalf("ComputeProjectID failed: %v", err)
	}
	cfgHash, err := ComputeConfigHash([]string{".git"}, true, 64*1024, 1)
	if err != nil {
		t.Fatalf("ComputeConfigHash failed: %v", err)
	}

	c := NewCache(tmp, pid, cfgHash, map[string]FileRef{
		"a.txt": {Size: 1, MTime: 123, Hash: "sha256:deadbeef"},
	})
	if err := SaveCache(cachePath, c); err != nil {
		t.Fatalf("SaveCache failed: %v", err)
	}

	loaded, ok, err := LoadCacheIfExists(cachePath)
	if err != nil {
		t.Fatalf("LoadCacheIfExists failed: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if loaded.Version != CacheVersion {
		t.Fatalf("unexpected version: %d", loaded.Version)
	}
	if loaded.ProjectRoot != tmp {
		t.Fatalf("unexpected project_root: %q", loaded.ProjectRoot)
	}
	if loaded.ProjectID != pid {
		t.Fatalf("unexpected project_id: %q", loaded.ProjectID)
	}
	if loaded.ConfigHash != cfgHash {
		t.Fatalf("unexpected config_hash: %q", loaded.ConfigHash)
	}
	ref, ok := loaded.Files["a.txt"]
	if !ok {
		t.Fatalf("expected file ref")
	}
	if ref.Size != 1 || ref.MTime != 123 || ref.Hash != "sha256:deadbeef" {
		t.Fatalf("unexpected file ref: %+v", ref)
	}
}

func TestLoadCacheIfExists_Missing(t *testing.T) {
	_, ok, err := LoadCacheIfExists(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false")
	}
}

func TestLoadCacheIfExists_BrokenJSON(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "cache.json")
	if err := os.WriteFile(path, []byte("{\n  \"version\": 1,"), 0644); err != nil {
		t.Fatalf("failed to write broken file: %v", err)
	}
	_, ok, err := LoadCacheIfExists(path)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !ok {
		t.Fatalf("expected ok=true for existing file")
	}
}

func TestSaveCache_OverwritesExisting(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "cache.json")

	pid, _ := ComputeProjectID(tmp)
	cfgHash, _ := ComputeConfigHash([]string{".git"}, true, 64*1024, 1)

	c1 := NewCache(tmp, pid, cfgHash, map[string]FileRef{"a.txt": {Size: 1, MTime: 1, Hash: "sha256:1"}})
	if err := SaveCache(path, c1); err != nil {
		t.Fatalf("SaveCache failed: %v", err)
	}

	c2 := NewCache(tmp, pid, cfgHash, map[string]FileRef{"b.txt": {Size: 2, MTime: 2, Hash: "sha256:2"}})
	if err := SaveCache(path, c2); err != nil {
		t.Fatalf("SaveCache failed: %v", err)
	}

	loaded, ok, err := LoadCacheIfExists(path)
	if err != nil {
		t.Fatalf("LoadCacheIfExists failed: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if _, ok := loaded.Files["b.txt"]; !ok {
		t.Fatalf("expected b.txt")
	}
	if _, ok := loaded.Files["a.txt"]; ok {
		t.Fatalf("did not expect a.txt")
	}
}
