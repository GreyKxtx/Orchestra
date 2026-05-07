package applier

import (
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
