package decisions

import (
	"fmt"
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

// Past the cap the older entries move to the archive, whole, and the log
// keeps its header, a note and the newest entries; nothing is lost.
func TestAppend_ArchivesOlderEntriesPastTheCap(t *testing.T) {
	old := maxBytes
	maxBytes = 1500
	t.Cleanup(func() { maxBytes = old })
	root := t.TempDir()
	for i := 0; i < 40; i++ {
		e := Entry{Kind: "qa", Dept: "backend", Question: fmt.Sprintf("q%02d: keep the history of this thing?", i), Answer: "yes, twenty-four months"}
		if err := Append(root, []Entry{e}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	log, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(FileRel)))
	if err != nil {
		t.Fatal(err)
	}
	if len(log) > maxBytes {
		t.Fatalf("log is %d bytes, past the cap of %d", len(log), maxBytes)
	}
	if !strings.HasPrefix(string(log), "# Decision log") || strings.Count(string(log), archiveNote) != 1 {
		t.Fatalf("header or note wrong:\n%s", log)
	}
	if !strings.Contains(string(log), "q39:") || strings.Contains(string(log), "q00:") {
		t.Fatalf("the log must hold the newest entry and not the oldest:\n%s", log)
	}
	archive, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ArchiveFileRel)))
	if err != nil {
		t.Fatalf("no archive: %v", err)
	}
	if !strings.Contains(string(archive), "q00:") || !strings.HasPrefix(string(archive), "# Decision log archive") {
		t.Fatalf("archive lacks the oldest entry or its header:\n%s", archive)
	}
	// Every entry is in exactly one of the two files.
	for i := 0; i < 40; i++ {
		q := fmt.Sprintf("Q: q%02d:", i)
		if n := strings.Count(string(log), q) + strings.Count(string(archive), q); n != 1 {
			t.Errorf("entry %d appears %d times across log and archive", i, n)
		}
	}
	if tail := Tail(root, 400); !strings.Contains(tail, "q39:") {
		t.Fatalf("Tail lost the newest entry: %q", tail)
	}
}
