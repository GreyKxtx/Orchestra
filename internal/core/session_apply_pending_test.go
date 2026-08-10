package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/patch/cache"
	"github.com/orchestra/orchestra/patch/ops"
)

func TestFilterPendingOpsByPaths(t *testing.T) {
	all := []ops.AnyOp{
		{Op: ops.OpFileWriteAtomic, Path: "internal/a.go"},
		{Op: ops.OpFileWriteAtomic, Path: "internal/b.go"},
	}
	toApply, remaining := filterPendingOpsByPaths(all, []string{"internal/a.go"})
	if len(toApply) != 1 || len(remaining) != 1 {
		t.Fatalf("want 1 apply + 1 remain, got %d + %d", len(toApply), len(remaining))
	}
	if toApply[0].Path != "internal/a.go" {
		t.Fatalf("wrong apply path: %q", toApply[0].Path)
	}
}

func TestFilterPendingOpsByPaths_emptyPathsAppliesAll(t *testing.T) {
	all := []ops.AnyOp{{Op: ops.OpFileWriteAtomic, Path: "x.go"}}
	toApply, remaining := filterPendingOpsByPaths(all, nil)
	if len(toApply) != 1 || len(remaining) != 0 {
		t.Fatalf("want all applied, got %d remain %d", len(toApply), len(remaining))
	}
}

func TestPendingPathMatches_suffixAndBase(t *testing.T) {
	if !pendingPathMatches("internal/agent/foo.go", []string{"foo.go"}) {
		t.Fatal("expected basename match")
	}
	if !pendingPathMatches("internal/agent/foo.go", []string{"agent/foo.go"}) {
		t.Fatal("expected suffix match")
	}
}

func TestFilterStagedPaths(t *testing.T) {
	staged := []string{"src/App.jsx", "src/main.jsx"}
	got := filterStagedPaths(staged, []string{"App.jsx"})
	if len(got) != 1 || got[0] != "src/App.jsx" {
		t.Fatalf("filterStagedPaths: %v", got)
	}
}

func TestRefreshPendingWriteHashes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("before"), 0644); err != nil {
		t.Fatal(err)
	}
	want := cache.ComputeSHA256([]byte("before"))
	wa := &ops.WriteAtomicOp{
		Op:      ops.OpFileWriteAtomic,
		Path:    "a.txt",
		Content: "after",
		Conditions: ops.WriteAtomicConditions{
			FileHash: "sha256:deadbeef",
		},
	}
	pending := []ops.AnyOp{{Op: wa.Op, Path: wa.Path, WriteAtomic: wa}}
	refreshPendingWriteHashes(dir, pending)
	if pending[0].WriteAtomic.Conditions.FileHash != want {
		t.Fatalf("hash: got %q want %q", pending[0].WriteAtomic.Conditions.FileHash, want)
	}
}
