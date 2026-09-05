package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeImportFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExpandImports_NoImportsReturnsUnchanged(t *testing.T) {
	dir := t.TempDir()
	got := expandImports("plain content, no macro here", dir)
	if got != "plain content, no macro here" {
		t.Errorf("got %q", got)
	}
}

func TestExpandImports_BasicSubstitution(t *testing.T) {
	dir := t.TempDir()
	writeImportFile(t, filepath.Join(dir, "arch.md"), "ARCHITECTURE NOTES")

	got := expandImports("See @import arch.md for details.", dir)

	if !strings.Contains(got, "ARCHITECTURE NOTES") {
		t.Fatalf("import not expanded: %q", got)
	}
	if strings.Contains(got, "@import") {
		t.Errorf("literal @import macro leaked into output: %q", got)
	}
}

// A file two levels deep must resolve its own @import against ITS OWN
// directory, not the root file's — otherwise moving a doc tree breaks every
// import inside it.
func TestExpandImports_RelativeToImportingFile(t *testing.T) {
	dir := t.TempDir()
	writeImportFile(t, filepath.Join(dir, "sub", "b.md"), "@import c.md")
	writeImportFile(t, filepath.Join(dir, "sub", "c.md"), "DEEP CONTENT")
	// c.md lives next to b.md (dir/sub/c.md), NOT next to the root file.

	got := expandImports("@import sub/b.md", dir)

	if !strings.Contains(got, "DEEP CONTENT") {
		t.Fatalf("nested relative import failed: %q", got)
	}
}

func TestExpandImports_CycleIsReportedNotInfiniteLoop(t *testing.T) {
	dir := t.TempDir()
	writeImportFile(t, filepath.Join(dir, "a.md"), "@import b.md")
	writeImportFile(t, filepath.Join(dir, "b.md"), "@import a.md")

	got := expandImports("@import a.md", dir)

	if !strings.Contains(got, "cycle") {
		t.Fatalf("expected a cycle marker in output, got: %q", got)
	}
}

func TestExpandImports_MaxDepthExceeded(t *testing.T) {
	dir := t.TempDir()
	// A straight-line chain one level deeper than the cap.
	writeImportFile(t, filepath.Join(dir, "l1.md"), "@import l2.md")
	writeImportFile(t, filepath.Join(dir, "l2.md"), "@import l3.md")
	writeImportFile(t, filepath.Join(dir, "l3.md"), "@import l4.md")
	writeImportFile(t, filepath.Join(dir, "l4.md"), "TOO DEEP")

	got := expandImports("@import l1.md", dir)

	if strings.Contains(got, "TOO DEEP") {
		t.Fatalf("depth cap did not stop expansion: %q", got)
	}
	if !strings.Contains(got, "depth") {
		t.Fatalf("expected a depth-limit marker, got: %q", got)
	}
}

func TestExpandImports_MissingFileLeavesMarkerNotCrash(t *testing.T) {
	dir := t.TempDir()

	got := expandImports("before @import does-not-exist.md after", dir)

	if !strings.Contains(got, "before") || !strings.Contains(got, "after") {
		t.Fatalf("surrounding content lost: %q", got)
	}
	if !strings.Contains(got, "import error") {
		t.Fatalf("expected a visible error marker, got: %q", got)
	}
}

func TestExpandImports_OneBadImportDoesNotDropOthers(t *testing.T) {
	dir := t.TempDir()
	writeImportFile(t, filepath.Join(dir, "good.md"), "GOOD CONTENT")

	got := expandImports("@import missing.md\n@import good.md", dir)

	if !strings.Contains(got, "GOOD CONTENT") {
		t.Fatalf("a failing import must not take down a sibling import: %q", got)
	}
}
