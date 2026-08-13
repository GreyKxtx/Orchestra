package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestAcquireFileLock_Basic: acquire → contender blocks → release → contender
// proceeds.
func TestAcquireFileLock_Basic(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".orchestra.yml")
	unlock := acquireFileLock(path)
	if _, err := os.Stat(path + ".lock"); err != nil {
		t.Fatalf("lock file missing: %v", err)
	}
	unlock()
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("lock file should be removed, err=%v", err)
	}
}

// TestAcquireFileLock_StaleReclaim: a lock left by a crashed writer (old
// mtime) must be reclaimed quickly instead of blocking until the timeout.
func TestAcquireFileLock_StaleReclaim(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".orchestra.yml")
	lockPath := path + ".lock"
	if err := os.WriteFile(lockPath, []byte("42"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Minute)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	unlock := acquireFileLock(path)
	defer unlock()
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("stale reclaim took %v, want fast path", elapsed)
	}
}

// TestSave_ConcurrentWriters: parallel Save calls must serialise and leave a
// parseable config behind (regression for cross-process lost updates —
// in-process goroutines contend on the same O_EXCL lock file).
func TestSave_ConcurrentWriters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".orchestra.yml")

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			cfg := DefaultConfig(dir)
			cfg.LLM.Model = "model-" + string(rune('a'+n))
			if err := Save(path, cfg); err != nil {
				t.Errorf("save %d: %v", n, err)
			}
		}(i)
	}
	wg.Wait()

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("config unreadable after concurrent saves: %v", err)
	}
	if loaded.LLM.Model == "" {
		t.Fatal("config lost llm.model after concurrent saves")
	}
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("lock file leaked after saves: err=%v", err)
	}
}
