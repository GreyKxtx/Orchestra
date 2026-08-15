package lessons

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSearch(t *testing.T) {
	root := t.TempDir()
	if err := Append(root, Entry{
		Dept:   "engineering",
		Kind:   KindPattern,
		Task:   "wire retry helper",
		Verify: "passed",
	}); err != nil {
		t.Fatal(err)
	}
	hits := Search(root, "retry", 5)
	if len(hits) != 1 || hits[0].Dept != "engineering" {
		t.Fatalf("hits = %+v", hits)
	}
	path := filepath.Join(root, filepath.FromSlash(RelDir), "backend.md")
	if err := os.WriteFile(path, []byte("\n## 2026-01-01 · pattern · backend\n- task: cache layer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Search(root, "cache", 5); len(got) != 1 {
		t.Fatalf("cache hits = %+v", got)
	}
}
