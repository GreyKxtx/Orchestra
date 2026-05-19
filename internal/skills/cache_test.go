package skills

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestDiscoverCached_Memoises(t *testing.T) {
	// Counts behaviour, not absolute size — DiscoverCached calls the
	// production Discover which also scans the user-global ~/.orchestra
	// skills, so we measure deltas between calls.
	dir := t.TempDir()
	projDir := filepath.Join(dir, ".orchestra", "skills")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "alpha-test.md"), []byte("---\nname: alpha-test-m9\ndescription: a\n---\nbody"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	InvalidateCache("")
	first, err := DiscoverCached(dir)
	if err != nil {
		t.Fatalf("first DiscoverCached: %v", err)
	}
	firstCount := len(first)

	// Add a second skill on disk — the cache should still return the
	// old slice until invalidated.
	if err := os.WriteFile(filepath.Join(projDir, "beta-test.md"), []byte("---\nname: beta-test-m9\ndescription: b\n---\nbody"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cached, err := DiscoverCached(dir)
	if err != nil {
		t.Fatalf("cached DiscoverCached: %v", err)
	}
	if len(cached) != firstCount {
		t.Errorf("cache must shield mid-process changes: got %d, want %d", len(cached), firstCount)
	}

	// After invalidation, the next read sees the second skill.
	InvalidateCache(dir)
	fresh, err := DiscoverCached(dir)
	if err != nil {
		t.Fatalf("fresh DiscoverCached: %v", err)
	}
	if len(fresh) != firstCount+1 {
		t.Errorf("after invalidate: want %d, got %d", firstCount+1, len(fresh))
	}
}

func TestDiscoverCached_ConcurrentSafe(t *testing.T) {
	dir := t.TempDir()
	InvalidateCache("")
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = DiscoverCached(dir)
		}()
	}
	wg.Wait()
}
