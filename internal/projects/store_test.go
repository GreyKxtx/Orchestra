package projects

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/orchestra/orchestra/patch/cache"
)

func TestStore_RoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "projects.json")
	want := []string{`C:\a\one`, `C:\b\two`}

	if err := SavePaths(p, want); err != nil {
		t.Fatalf("SavePaths: %v", err)
	}
	got, err := LoadPaths(p)
	if err != nil {
		t.Fatalf("LoadPaths: %v", err)
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("round trip = %v, want %v", got, want)
	}

	// Windows does not model these bits; skip the assertion, not the test.
	if runtime.GOOS != "windows" {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if perm := st.Mode().Perm(); perm != 0600 {
			t.Fatalf("mode = %o, want 600", perm)
		}
	}
}

func TestStore_AbsentFileIsNotAnError(t *testing.T) {
	got, err := LoadPaths(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("a first run must not fail: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v, want empty", got)
	}
}

func TestStore_CorruptFileIsNotFatal(t *testing.T) {
	p := filepath.Join(t.TempDir(), "projects.json")
	if err := os.WriteFile(p, []byte("{not json"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := LoadPaths(p)
	if err != nil {
		t.Fatalf("a corrupt list must not stop the server from starting: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v, want empty", got)
	}
}

func TestStorePath_IsUnderTheOrchestraHome(t *testing.T) {
	p, err := StorePath()
	if err != nil {
		t.Fatalf("StorePath: %v", err)
	}
	if filepath.Base(p) != "projects.json" {
		t.Fatalf("StorePath = %q, want it to end in projects.json", p)
	}
	if filepath.Base(filepath.Dir(p)) != ".orchestra" {
		t.Fatalf("StorePath = %q, want it under ~/.orchestra", p)
	}
}

// A read failure that is not "no such file" must surface: otherwise a transient
// permission error reads as an empty list, and the very next SavePaths
// overwrites the user's real list with nothing.
func TestStore_UnreadableFileIsAnError(t *testing.T) {
	dir := t.TempDir() // a directory where a file is expected → ReadFile fails, not ENOENT
	if _, err := LoadPaths(dir); err == nil {
		t.Fatal("LoadPaths on an unreadable path returned nil error; the caller would overwrite the list")
	}
}

func TestStore_AddIsIdempotentByProjectID(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "projects.json")
	proj := t.TempDir()

	s, err := NewStore(file)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := s.Add(proj); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.Add(proj); err != nil {
		t.Fatalf("Add again: %v", err)
	}
	if got := s.Paths(); len(got) != 1 {
		t.Fatalf("want 1 remembered path, got %d: %v", len(got), got)
	}
}

func TestStore_AddPersistsAndReloads(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "projects.json")
	proj := t.TempDir()

	s, err := NewStore(file)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := s.Add(proj); err != nil {
		t.Fatalf("Add: %v", err)
	}

	again, err := NewStore(file)
	if err != nil {
		t.Fatalf("NewStore reload: %v", err)
	}
	got := again.Paths()
	if len(got) != 1 || got[0] != filepath.Clean(proj) {
		t.Fatalf("reloaded list is %v, want [%s]", got, filepath.Clean(proj))
	}
}

func TestStore_ForgetRemovesAndReportsWhetherItWasThere(t *testing.T) {
	file := filepath.Join(t.TempDir(), "projects.json")
	proj := t.TempDir()

	s, err := NewStore(file)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := s.Add(proj); err != nil {
		t.Fatalf("Add: %v", err)
	}

	had, err := s.Forget(proj)
	if err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if !had {
		t.Fatal("Forget reported the path was not remembered, but it was added")
	}
	if got := s.Paths(); len(got) != 0 {
		t.Fatalf("want an empty list after Forget, got %v", got)
	}

	had, err = s.Forget(proj)
	if err != nil {
		t.Fatalf("Forget again: %v", err)
	}
	if had {
		t.Fatal("Forget reported a path it had already removed")
	}
}

func TestStore_PathForIDResolvesARememberedProject(t *testing.T) {
	file := filepath.Join(t.TempDir(), "projects.json")
	proj := t.TempDir()

	s, err := NewStore(file)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := s.Add(proj); err != nil {
		t.Fatalf("Add: %v", err)
	}

	abs, _ := filepath.Abs(proj)
	id, err := cache.ComputeProjectID(abs)
	if err != nil {
		t.Fatalf("ComputeProjectID: %v", err)
	}
	got, ok := s.PathForID(id)
	if !ok {
		t.Fatalf("PathForID(%q) found nothing; list is %v", id, s.Paths())
	}
	if got != filepath.Clean(abs) {
		t.Fatalf("PathForID returned %q, want %q", got, filepath.Clean(abs))
	}

	if _, ok := s.PathForID("nope"); ok {
		t.Fatal("PathForID invented a path for an unknown id")
	}
}

func TestNewStore_UnreadableListIsAnError(t *testing.T) {
	// A directory where the file should be: readable path, unreadable content.
	// The caller must learn this rather than silently starting with an empty
	// list and overwriting a list it never saw.
	dir := t.TempDir()
	inTheWay := filepath.Join(dir, "projects.json")
	if err := os.Mkdir(inTheWay, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := NewStore(inTheWay); err == nil {
		t.Fatal("NewStore accepted an unreadable list")
	}
}
