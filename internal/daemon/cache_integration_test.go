package daemon

import (
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/store"
)

func TestNewServer_IgnoresCacheOnConfigHashMismatch(t *testing.T) {
	root := t.TempDir()

	pid, err := store.ComputeProjectID(root)
	if err != nil {
		t.Fatalf("ComputeProjectID failed: %v", err)
	}

	cachePath := filepath.Join(root, ".orchestra", "cache.json")
	c := store.NewCache(root, pid, "sha256:wrong-config", map[string]store.FileRef{
		"ghost.txt": {Size: 1, MTime: 1, Hash: "sha256:deadbeef"},
	})
	if err := store.SaveCache(cachePath, c); err != nil {
		t.Fatalf("SaveCache failed: %v", err)
	}

	srv, err := NewServer(ServerConfig{
		ProjectRoot:       root,
		Address:           "127.0.0.1",
		Port:              0,
		ExcludeDirs:       []string{".git", ".orchestra"},
		SkipBackups:       true,
		ScanInterval:      DefaultScanInterval,
		MaxCacheFileBytes: DefaultMaxCacheFileBytes,
		CacheEnabled:      true,
		CachePath:         ".orchestra/cache.json",
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// If cache was ignored, state should not be pre-seeded with ghost.txt.
	if len(srv.state.files) != 0 {
		t.Fatalf("expected empty seeded state, got %d files", len(srv.state.files))
	}
}
