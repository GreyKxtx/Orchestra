package decisions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendAndTail(t *testing.T) {
	root := t.TempDir()
	if Tail(root, 4096) != "" {
		t.Fatal("no log → empty tail")
	}
	if err := Append(root, []Entry{{Kind: "qa", Dept: "backend", Question: "keep history?", Answer: "24 months"}}); err != nil {
		t.Fatalf("append: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(FileRel)))
	if err != nil {
		t.Fatal(err)
	}
	first := string(data)
	if !strings.HasPrefix(first, "# Decision log") {
		t.Fatal("first append must write the header")
	}
	if !strings.Contains(first, "Q: keep history?") || !strings.Contains(first, "A: 24 months") {
		t.Fatalf("Q/A missing:\n%s", first)
	}

	// Append-only: previous content survives verbatim.
	if err := Append(root, []Entry{{Kind: "assumption", Question: "tz?", Answer: "UTC"}}); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(root, filepath.FromSlash(FileRel)))
	if !strings.HasPrefix(string(data), first) {
		t.Fatal("append must not rewrite existing content")
	}

	tail := Tail(root, 60)
	if tail == "" || len(tail) > 60+120 {
		t.Fatalf("bounded tail expected, got %d bytes", len(tail))
	}
	if !strings.Contains(tail, "UTC") {
		t.Fatalf("tail must contain the newest entry: %q", tail)
	}
}

func TestAdopted(t *testing.T) {
	root := t.TempDir()
	if Adopted(root) {
		t.Fatal("no state.md → not adopted")
	}
	if err := os.MkdirAll(filepath.Join(root, ".orchestra"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".orchestra", "state.md"), []byte("---\norchestra:\n  phase: discovery\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !Adopted(root) {
		t.Fatal("state.md present → adopted")
	}
}
