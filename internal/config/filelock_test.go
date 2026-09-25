package config

import (
	"os"
	"path/filepath"
	"strings"
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

// UpdateFile holds the config lock across read → fn → write, so concurrent
// edits from several writers all land, and nothing of the write is left
// behind: no temp file, no lock.
func TestUpdateFile_SerialisesWritersAndLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".orchestra.yml")
	if err := os.WriteFile(path, []byte("project_root: .\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const writers = 8
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- UpdateFile(path, func(current []byte) ([]byte, error) {
				time.Sleep(2 * time.Millisecond) // widen the read→write window
				return append(current, []byte("key"+string(rune('a'+i))+": v\n")...), nil
			})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("UpdateFile: %v", err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < writers; i++ {
		want := "key" + string(rune('a'+i)) + ": v\n"
		if !strings.Contains(string(data), want) {
			t.Errorf("writer %d's edit was lost:\n%s", i, data)
		}
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != ".orchestra.yml" {
			t.Errorf("left behind: %s", e.Name())
		}
	}
}

// A writer that holds the lock keeps UpdateFile waiting: the model command
// used to write straight over the config while a Save was in flight.
func TestUpdateFile_WaitsForTheLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".orchestra.yml")
	if err := os.WriteFile(path, []byte("a: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	unlock := acquireFileLock(path)
	done := make(chan error, 1)
	go func() {
		done <- UpdateFile(path, func(current []byte) ([]byte, error) { return []byte("a: 2\n"), nil })
	}()
	select {
	case err := <-done:
		t.Fatalf("UpdateFile returned (%v) while the lock was held", err)
	case <-time.After(150 * time.Millisecond):
	}
	unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("UpdateFile after unlock: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("UpdateFile never proceeded after the lock was released")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "a: 2\n" {
		t.Fatalf("config = %q", data)
	}
}
