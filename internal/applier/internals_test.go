package applier

import (
	"os"
	"testing"

	"github.com/orchestra/orchestra/internal/ops"
)

// ---- fuzzyFindInWindow ----

func TestFuzzyFindInWindow_EmptyContent(t *testing.T) {
	_, _, matches, err := fuzzyFindInWindow([]byte{}, "needle", 0, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matches != 0 {
		t.Fatalf("expected 0 matches, got %d", matches)
	}
}

func TestFuzzyFindInWindow_NoMatch(t *testing.T) {
	content := []byte("func A() {}\nfunc B() {}\n")
	_, _, matches, err := fuzzyFindInWindow(content, "func Z() {}", 0, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matches != 0 {
		t.Fatalf("expected 0 matches, got %d", matches)
	}
}

func TestFuzzyFindInWindow_UniqueMatch(t *testing.T) {
	content := []byte("func A() {}\nfunc B() { target }\nfunc C() {}\n")
	start, end, matches, err := fuzzyFindInWindow(content, "func B() { target }", 0, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matches != 1 {
		t.Fatalf("expected 1 match, got %d", matches)
	}
	if start >= end {
		t.Fatalf("invalid match range [%d, %d)", start, end)
	}
}

func TestFuzzyFindInWindow_AmbiguousMatch(t *testing.T) {
	content := []byte("dup\ndup\n")
	_, _, matches, err := fuzzyFindInWindow(content, "dup", 0, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matches < 2 {
		t.Fatalf("expected ≥2 matches for ambiguous case, got %d", matches)
	}
}

func TestFuzzyFindInWindow_WindowLimitsSearch(t *testing.T) {
	// Target is at line 10, window=1, so lines 9-11 only.
	// Build content with "unique_target" only at line 10 but search starts at line 0 with window=1.
	lines := make([]byte, 0, 256)
	for i := 0; i < 20; i++ {
		if i == 10 {
			lines = append(lines, []byte("unique_target\n")...)
		} else {
			lines = append(lines, []byte("other_line\n")...)
		}
	}
	// Start at line 10 with window=1 → lines 9-11, should find it.
	_, _, matches, _ := fuzzyFindInWindow(lines, "unique_target", 10, 1)
	if matches != 1 {
		t.Fatalf("expected 1 match when target is in window, got %d", matches)
	}
	// Start at line 0 with window=1 → lines 0-1, should not find it.
	_, _, matches2, _ := fuzzyFindInWindow(lines, "unique_target", 0, 1)
	if matches2 != 0 {
		t.Fatalf("expected 0 matches outside window, got %d", matches2)
	}
}

// ---- offsetForPos ----

func TestOffsetForPos_LineZeroColZero(t *testing.T) {
	content := []byte("hello\nworld\n")
	lineStarts := computeLineStarts(content)
	off, err := offsetForPos(lineStarts, content, ops.Position{Line: 0, Col: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if off != 0 {
		t.Fatalf("expected offset 0, got %d", off)
	}
}

func TestOffsetForPos_LineOutOfRange(t *testing.T) {
	content := []byte("hello\n")
	lineStarts := computeLineStarts(content)
	_, err := offsetForPos(lineStarts, content, ops.Position{Line: 99, Col: 0})
	if err == nil {
		t.Fatal("expected error for out-of-range line")
	}
}

func TestOffsetForPos_ColPastLineEnd(t *testing.T) {
	content := []byte("hi\n")
	lineStarts := computeLineStarts(content)
	// col=100 is past the line — function returns an error
	_, err := offsetForPos(lineStarts, content, ops.Position{Line: 0, Col: 100})
	if err == nil {
		t.Fatal("expected error for col past line end")
	}
}

// ---- preview ----

func TestPreview_Short(t *testing.T) {
	s := "hello"
	if got := preview(s, 10); got != s {
		t.Errorf("got %q, want %q", got, s)
	}
}

func TestPreview_Truncated(t *testing.T) {
	s := "hello world this is long"
	got := preview(s, 5)
	// Result should start with the first 5 chars and contain a truncation marker.
	if len(got) >= len(s) {
		t.Errorf("expected shorter string, got %q (len %d)", got, len(got))
	}
	if got[:5] != s[:5] {
		t.Errorf("expected prefix %q, got %q", s[:5], got[:5])
	}
}

// ---- max ----

func TestMax_BGreaterThanA(t *testing.T) {
	if got := max(1, 5); got != 5 {
		t.Errorf("max(1,5) = %d, want 5", got)
	}
}

func TestMax_AGreaterThanB(t *testing.T) {
	if got := max(7, 3); got != 7 {
		t.Errorf("max(7,3) = %d, want 7", got)
	}
}

// ---- fuzzyFindInWindow negative window ----

func TestFuzzyFindInWindow_NegativeWindow(t *testing.T) {
	_, _, _, err := fuzzyFindInWindow([]byte("hello\n"), "hello", 0, -1)
	if err == nil {
		t.Fatal("expected error for negative window")
	}
}

// ---- offsetForPos negative position ----

func TestOffsetForPos_NegativePosition(t *testing.T) {
	content := []byte("hello\n")
	ls := computeLineStarts(content)
	_, err := offsetForPos(ls, content, ops.Position{Line: -1, Col: 0})
	if err == nil {
		t.Fatal("expected error for negative line")
	}
	_, err = offsetForPos(ls, content, ops.Position{Line: 0, Col: -1})
	if err == nil {
		t.Fatal("expected error for negative col")
	}
}

// ---- offsetsForRange end before start ----

func TestOffsetsForRange_EndBeforeStart(t *testing.T) {
	content := []byte("line0\nline1\nline2\n")
	// End line 0 < Start line 2 — should fail.
	_, _, err := offsetsForRange(content, ops.Range{
		Start: ops.Position{Line: 2, Col: 0},
		End:   ops.Position{Line: 0, Col: 0},
	})
	if err == nil {
		t.Fatal("expected error when range end precedes start")
	}
}

// ---- applyReplaceRangeOps error paths ----

func TestApplyReplaceRangeOps_WrongOpType(t *testing.T) {
	content := []byte("foo\nbar\n")
	opList := []ops.ReplaceRangeOp{
		{
			Op:   "file.wrong_op",
			Path: "x.go",
			Range: ops.Range{
				Start: ops.Position{Line: 0, Col: 0},
				End:   ops.Position{Line: 0, Col: 3},
			},
			Expected:    "foo",
			Replacement: "baz",
		},
	}
	_, err := applyReplaceRangeOps("x.go", content, opList)
	if err == nil {
		t.Fatal("expected error for wrong op type")
	}
}

func TestApplyReplaceRangeOps_EmptyPath(t *testing.T) {
	content := []byte("foo\n")
	opList := []ops.ReplaceRangeOp{
		{
			Op:   ops.OpFileReplaceRange,
			Path: "",
			Range: ops.Range{
				Start: ops.Position{Line: 0, Col: 0},
				End:   ops.Position{Line: 0, Col: 3},
			},
			Expected:    "foo",
			Replacement: "bar",
		},
	}
	_, err := applyReplaceRangeOps("x.go", content, opList)
	if err == nil {
		t.Fatal("expected error for empty op path")
	}
}

func TestApplyReplaceRangeOps_NegativeRange(t *testing.T) {
	content := []byte("foo\n")
	opList := []ops.ReplaceRangeOp{
		{
			Op:   ops.OpFileReplaceRange,
			Path: "x.go",
			Range: ops.Range{
				Start: ops.Position{Line: -1, Col: 0},
				End:   ops.Position{Line: 0, Col: 3},
			},
			Expected:    "foo",
			Replacement: "bar",
		},
	}
	_, err := applyReplaceRangeOps("x.go", content, opList)
	if err == nil {
		t.Fatal("expected error for negative range")
	}
}

func TestApplyReplaceRangeOps_FuzzyNotFound(t *testing.T) {
	content := []byte("alpha\nbeta\n")
	opList := []ops.ReplaceRangeOp{
		{
			Op:   ops.OpFileReplaceRange,
			Path: "x.go",
			Range: ops.Range{
				Start: ops.Position{Line: 0, Col: 0},
				End:   ops.Position{Line: 0, Col: 5},
			},
			Expected:    "ZZZNOMATCH",
			Replacement: "new",
			Conditions:  ops.Conditions{AllowFuzzy: true, FuzzyWindow: 5},
		},
	}
	_, err := applyReplaceRangeOps("x.go", content, opList)
	if err == nil {
		t.Fatal("expected stale error when fuzzy finds no match")
	}
}

// ---- atomicWriteFile ----

func TestAtomicWriteFile_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/out.txt"
	if err := atomicWriteFile(path, []byte("hello"), 0644, ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "hello" {
		t.Fatalf("content: %q, err: %v", b, err)
	}
}

func TestAtomicWriteFile_Overwrite(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/out.txt"
	_ = os.WriteFile(path, []byte("old"), 0644)
	if err := atomicWriteFile(path, []byte("new"), 0644, ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "new" {
		t.Errorf("content: %q, want %q", b, "new")
	}
}

func TestAtomicWriteFile_WithRootCheck(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/sub/out.txt"
	if err := atomicWriteFile(path, []byte("data"), 0644, dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "data" {
		t.Errorf("content: %q", b)
	}
}

func TestAtomicWriteFile_SymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	// Create a dir symlink inside root that points outside.
	linkDir := root + "/linked"
	if err := os.Symlink(outside, linkDir); err != nil {
		t.Skipf("symlink not supported/allowed: %v", err)
	}

	path := linkDir + "/secret.txt"
	err := atomicWriteFile(path, []byte("evil"), 0644, root)
	if err == nil {
		t.Fatal("expected error for symlink escape in atomicWriteFile")
	}
	// File should not exist outside root.
	if _, statErr := os.Stat(outside + "/secret.txt"); !os.IsNotExist(statErr) {
		t.Fatal("file should not be created outside workspace")
	}
}

// ---- safeAbsPath: symlink file target ----

func TestSafeAbsPath_SymlinkFileRejected(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	realFile := outside + "/real.go"
	_ = os.WriteFile(realFile, []byte("x"), 0644)

	linkFile := root + "/link.go"
	if err := os.Symlink(realFile, linkFile); err != nil {
		t.Skipf("symlink not supported/allowed: %v", err)
	}

	rootAbs := root
	_, err := safeAbsPath(rootAbs, rootAbs, "link.go")
	if err == nil {
		t.Fatal("expected error for symlink file target")
	}
}
