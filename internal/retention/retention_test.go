package retention

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPruneFiles_KeepsTheNewestByName(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"orchestra-20260101T000000.patch", "orchestra-20260102T000000.patch", "orchestra-20260103T000000.patch", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := PruneFiles(dir, ".patch", 2); got != 1 {
		t.Fatalf("removed %d, want 1", got)
	}
	left, _ := os.ReadDir(dir)
	var names []string
	for _, e := range left {
		names = append(names, e.Name())
	}
	want := []string{"notes.txt", "orchestra-20260102T000000.patch", "orchestra-20260103T000000.patch"}
	if len(names) != len(want) {
		t.Fatalf("left %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("left %v, want %v", names, want)
		}
	}
}

func TestPruneFiles_NegativeKeepAndMissingDirAreNoOps(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.patch"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := PruneFiles(dir, ".patch", -1); got != 0 {
		t.Fatalf("keep -1 removed %d", got)
	}
	if got := PruneFiles(filepath.Join(dir, "missing"), ".patch", 0); got != 0 {
		t.Fatalf("missing dir removed %d", got)
	}
}
