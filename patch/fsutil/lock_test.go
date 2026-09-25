package fsutil

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Read-modify-write under the lock loses nothing, however many writers race.
func TestLockFile_SerialisesReadModifyWrite(t *testing.T) {
	dir := t.TempDir()
	counter := filepath.Join(dir, "counter")
	lock := filepath.Join(dir, "counter.lock")
	if err := os.WriteFile(counter, []byte("0"), 0o644); err != nil {
		t.Fatal(err)
	}
	const writers = 40
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := LockFile(lock)
			if err != nil {
				t.Error(err)
				return
			}
			defer release()
			data, _ := os.ReadFile(counter)
			n, _ := strconv.Atoi(strings.TrimSpace(string(data)))
			if err := AtomicWriteFile(counter, []byte(strconv.Itoa(n+1)), 0o644); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	data, _ := os.ReadFile(counter)
	if got := strings.TrimSpace(string(data)); got != strconv.Itoa(writers) {
		t.Fatalf("counter = %s, want %d: an update was lost", got, writers)
	}
	release, err := LockFile(lock)
	if err != nil {
		t.Fatal(err)
	}
	release()
	release() // idempotent
}

// AtomicWriteFile keeps its hands off the permissions of a directory that is
// not Orchestra's own.
func TestAtomicWriteFile_LeavesOtherDirectoriesAlone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	dir := filepath.Join(t.TempDir(), "docs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteFile(filepath.Join(dir, "x.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("a user directory's mode changed to %o", got)
	}
	orch := filepath.Join(t.TempDir(), ".orchestra")
	if err := AtomicWriteFile(filepath.Join(orch, "plan.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(orch); info.Mode().Perm() != 0o700 {
		t.Fatalf(".orchestra stays private, got %o", info.Mode().Perm())
	}
}
