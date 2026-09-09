package projects

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
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
