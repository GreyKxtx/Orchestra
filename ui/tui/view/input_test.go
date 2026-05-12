package view

import (
	"strings"
	"testing"
)

func newTestInput(t *testing.T, value string) *Input {
	t.Helper()
	in := NewInput(80)
	in.SetValue(value)
	// SetValue places cursor at end of value; bring back to start for predictable tests.
	in.MoveCursorAbs(0)
	return &in
}

func TestInput_CursorPos_AfterMoveCursorAbs(t *testing.T) {
	in := newTestInput(t, "hello\nworld")
	in.MoveCursorAbs(7) // 'o' in "world" — pos 7 absolute
	if got := in.CursorPos(); got != 7 {
		t.Fatalf("CursorPos: want 7, got %d", got)
	}
}

func TestInput_DeleteSelection_PositionsCursorOnLo(t *testing.T) {
	in := newTestInput(t, "abcdef")
	in.MoveCursorAbs(1)
	in.SetAnchor(1)
	in.MoveCursorAbs(4) // selection [1, 4) → "bcd"
	if !in.DeleteSelection() {
		t.Fatal("DeleteSelection returned false")
	}
	if got := in.Value(); got != "aef" {
		t.Fatalf("Value: want %q, got %q", "aef", got)
	}
	if got := in.CursorPos(); got != 1 {
		t.Fatalf("CursorPos: want 1, got %d", got)
	}
	if in.HasSelection() {
		t.Fatal("selection should be cleared")
	}
}

func TestInput_ReplaceSelection_PositionsCursorAfterInsert(t *testing.T) {
	in := newTestInput(t, "abcdef")
	in.SetAnchor(1)
	in.MoveCursorAbs(4)
	if !in.ReplaceSelection("XYZ") {
		t.Fatal("ReplaceSelection returned false")
	}
	if got := in.Value(); got != "aXYZef" {
		t.Fatalf("Value: want %q, got %q", "aXYZef", got)
	}
	if got := in.CursorPos(); got != 4 {
		t.Fatalf("CursorPos: want 4 (after 'aXYZ'), got %d", got)
	}
	if in.HasSelection() {
		t.Fatal("selection should be cleared")
	}
}

func TestInput_ReplaceSelection_NoSelection_InsertsAtCursor(t *testing.T) {
	in := newTestInput(t, "abc")
	in.MoveCursorAbs(2)
	if in.ReplaceSelection("X") {
		t.Fatal("ReplaceSelection should return false when no selection")
	}
	if got := in.Value(); got != "abXc" {
		t.Fatalf("Value: want %q, got %q", "abXc", got)
	}
}

func TestInput_SelectAll_Empty_NoOp(t *testing.T) {
	in := newTestInput(t, "")
	in.SelectAll()
	if in.HasSelection() {
		t.Fatal("SelectAll on empty input should not create a selection")
	}
}

func TestInput_SelectAll_NonEmpty(t *testing.T) {
	in := newTestInput(t, "hello")
	in.SelectAll()
	lo, hi, ok := in.SelectionRange()
	if !ok || lo != 0 || hi != 5 {
		t.Fatalf("SelectAll: want [0,5), got [%d,%d) ok=%v", lo, hi, ok)
	}
}

func TestInput_WordRange_Middle(t *testing.T) {
	in := newTestInput(t, "hello world")
	lo, hi := in.WordRange(2)
	if lo != 0 || hi != 5 {
		t.Fatalf("WordRange(2): want [0,5), got [%d,%d)", lo, hi)
	}
}

func TestInput_WordRange_OnSpace(t *testing.T) {
	in := newTestInput(t, "hello world")
	lo, hi := in.WordRange(5) // pos on the space — fallback to left word
	if lo != 0 || hi != 5 {
		t.Fatalf("WordRange(5): want [0,5), got [%d,%d)", lo, hi)
	}
}

func TestInput_WordRange_AtEnd(t *testing.T) {
	in := newTestInput(t, "hello")
	lo, hi := in.WordRange(5)
	if lo != 0 || hi != 5 {
		t.Fatalf("WordRange(5): want [0,5), got [%d,%d)", lo, hi)
	}
}

func TestInput_LineRange_MultiLine(t *testing.T) {
	in := newTestInput(t, "a\nbcd\ne")
	// pos 3 is 'c' in "bcd" — line range [2, 5)
	lo, hi := in.LineRange(3)
	if lo != 2 || hi != 5 {
		t.Fatalf("LineRange(3): want [2,5), got [%d,%d)", lo, hi)
	}
}

func TestInput_Paste_WithSelection_ReplacesIt(t *testing.T) {
	in := newTestInput(t, "abc")
	in.SetAnchor(0)
	in.MoveCursorAbs(3)
	if !in.Paste("XY") {
		t.Fatal("Paste returned false")
	}
	if got := in.Value(); got != "XY" {
		t.Fatalf("Value: want %q, got %q", "XY", got)
	}
}

func TestInput_DeleteForward_NoSelection(t *testing.T) {
	in := newTestInput(t, "abc")
	in.MoveCursorAbs(1)
	in.DeleteForward()
	if got := in.Value(); got != "ac" {
		t.Fatalf("Value: want %q, got %q", "ac", got)
	}
	if got := in.CursorPos(); got != 1 {
		t.Fatalf("CursorPos: want 1, got %d", got)
	}
}

func TestInput_DeleteForward_WithSelection(t *testing.T) {
	in := newTestInput(t, "abcdef")
	in.SetAnchor(1)
	in.MoveCursorAbs(4)
	in.DeleteForward()
	if got := in.Value(); got != "aef" {
		t.Fatalf("Value: want %q, got %q", "aef", got)
	}
}

func TestInput_DeleteBackward_NoSelection(t *testing.T) {
	in := newTestInput(t, "abc")
	in.MoveCursorAbs(2)
	in.DeleteBackward()
	if got := in.Value(); got != "ac" {
		t.Fatalf("Value: want %q, got %q", "ac", got)
	}
	if got := in.CursorPos(); got != 1 {
		t.Fatalf("CursorPos: want 1, got %d", got)
	}
}

func TestInput_InsertNewline_SplitsLine(t *testing.T) {
	in := newTestInput(t, "abc")
	in.MoveCursorAbs(2)
	in.InsertNewline()
	if got := in.Value(); got != "ab\nc" {
		t.Fatalf("Value: want %q, got %q", "ab\nc", got)
	}
}

func TestInput_SyncHeight_Caps(t *testing.T) {
	in := newTestInput(t, "a\nb\nc\nd\ne\nf\ng")
	in.SyncHeight(5)
	if got := in.Inner().Height(); got != 5 {
		t.Fatalf("Height: want 5 (capped), got %d", got)
	}
}

func TestInput_SyncHeight_GrowsToLineCount(t *testing.T) {
	in := newTestInput(t, "a\nb\nc")
	in.SyncHeight(5)
	if got := in.Inner().Height(); got != 3 {
		t.Fatalf("Height: want 3, got %d", got)
	}
}

func TestInput_SelectToLineEnd_FromMiddle(t *testing.T) {
	in := newTestInput(t, "hello\nworld")
	in.MoveCursorAbs(2) // 'l' in "hello"
	in.SelectToLineEnd()
	lo, hi, ok := in.SelectionRange()
	if !ok || lo != 2 || hi != 5 {
		t.Fatalf("SelectionRange: want [2,5), got [%d,%d) ok=%v", lo, hi, ok)
	}
}

func TestInput_SelectToDocStart(t *testing.T) {
	in := newTestInput(t, "hello\nworld")
	in.MoveCursorAbs(8)
	in.SelectToDocStart()
	lo, hi, ok := in.SelectionRange()
	if !ok || lo != 0 || hi != 8 {
		t.Fatalf("SelectionRange: want [0,8), got [%d,%d) ok=%v", lo, hi, ok)
	}
}

func TestInput_Cut_RemovesAndReturns(t *testing.T) {
	in := newTestInput(t, "hello world")
	in.SetAnchor(0)
	in.MoveCursorAbs(5)
	got := in.Cut()
	if got != "hello" {
		t.Fatalf("Cut: want %q, got %q", "hello", got)
	}
	if v := in.Value(); v != " world" {
		t.Fatalf("Value after Cut: want %q, got %q", " world", v)
	}
}

func TestInput_isWordChar(t *testing.T) {
	// isWordChar (used by WordRange) handles unicode letters / digits / underscore.
	if !isWordChar('я') {
		t.Fatal("isWordChar should return true for Cyrillic letter")
	}
	if !isWordChar('_') {
		t.Fatal("isWordChar should return true for underscore")
	}
	if !isWordChar('7') {
		t.Fatal("isWordChar should return true for digit")
	}
	if isWordChar(' ') {
		t.Fatal("isWordChar should return false for space")
	}
}

// Sanity: WelcomeRender with mid-cursor still returns the same number of
// visible rows as logical lines (no extra row from cursor overlay).
func TestInput_WelcomeRender_RowCountMatchesLineCount(t *testing.T) {
	in := newTestInput(t, "ab\ncd\nef")
	out := in.WelcomeRender(20, true)
	rows := strings.Count(out, "\n") + 1
	if rows != 3 {
		t.Fatalf("WelcomeRender rows: want 3, got %d", rows)
	}
}
