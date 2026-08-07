package view

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/textarea"
)

func (in Input) HasSelection() bool { return in.selAnchor >= 0 }

// SelectionRange returns the [min, max) rune indices of the current selection
// and ok=true. Returns 0, 0, false when no selection is active.
func (in Input) SelectionRange() (min, max int, ok bool) {
	if in.selAnchor < 0 {
		return 0, 0, false
	}
	var cursor int
	if in.mouseCaretActive {
		cursor = in.mouseCaret
	} else {
		runes := []rune(in.ta.Value())
		cursor = clampPos(absolutePos(in.ta), len(runes))
	}
	a := in.selAnchor
	if a <= cursor {
		return a, cursor, true
	}
	return cursor, a, true
}

// SetAnchor pins the selection start to pos (call before moving cursor).
func (in *Input) SetAnchor(pos int) { in.selAnchor = pos }

// ClearSelection removes any active selection.
func (in *Input) ClearSelection() { in.selAnchor = -1 }

// MoveCursorAbs positions the cursor at the absolute rune index.
func (in *Input) MoveCursorAbs(pos int) { in.moveCursorAbs(pos) }

// SelectAll selects the entire value: anchor at 0, cursor at end.
// No-op if the value is empty.
func (in *Input) SelectAll() {
	runes := []rune(in.ta.Value())
	if len(runes) == 0 {
		return
	}
	in.selAnchor = 0
	in.moveCursorAbs(len(runes))
}

// SelectToLineStart extends the selection (or starts one at the current
// cursor) to the start of the current logical line.
func (in *Input) SelectToLineStart() {
	pos := in.CursorPos()
	if !in.HasSelection() {
		in.selAnchor = pos
	}
	lo, _ := in.currentLineRange(pos)
	in.moveCursorAbs(lo)
}

// SelectToLineEnd extends the selection to the end of the current logical line.
func (in *Input) SelectToLineEnd() {
	pos := in.CursorPos()
	if !in.HasSelection() {
		in.selAnchor = pos
	}
	_, hi := in.currentLineRange(pos)
	in.moveCursorAbs(hi)
}

// SelectToDocStart extends the selection to the start of the entire value.
func (in *Input) SelectToDocStart() {
	pos := in.CursorPos()
	if !in.HasSelection() {
		in.selAnchor = pos
	}
	in.moveCursorAbs(0)
}

// SelectToDocEnd extends the selection to the end of the entire value.
func (in *Input) SelectToDocEnd() {
	pos := in.CursorPos()
	if !in.HasSelection() {
		in.selAnchor = pos
	}
	runes := []rune(in.ta.Value())
	in.moveCursorAbs(len(runes))
}

// SelectToPos extends the selection to absolute pos, anchoring at the
// current cursor if no selection is active yet.
func (in *Input) SelectToPos(pos int) {
	if !in.HasSelection() {
		in.selAnchor = in.CursorPos()
	}
	in.moveCursorAbs(pos)
}

// ExtendSelectionTo keeps the existing anchor and moves the cursor to pos.
// If no anchor exists yet, sets it to the current cursor first. Used by
// Shift+click and similar gestures.
func (in *Input) ExtendSelectionTo(pos int) {
	if !in.HasSelection() {
		in.selAnchor = in.CursorPos()
	}
	in.moveCursorAbs(pos)
}

// SetMouseCaret sets the visual cursor to pos during mouse drag, bypassing textarea cursor.
func (in *Input) SetMouseCaret(pos int) {
	in.mouseCaret = pos
	in.mouseCaretActive = true
}

// MouseCaret returns the current mouse caret position.
func (in Input) MouseCaret() int { return in.mouseCaret }

// ClearMouseCaret deactivates the mouse caret, restoring textarea cursor display.
func (in *Input) ClearMouseCaret() { in.mouseCaretActive = false }

// DeleteSelection removes the selected rune range, clears the selection,
// and positions the cursor at the start of the deleted range. Returns true
// if anything was deleted.
func (in *Input) DeleteSelection() bool {
	lo, hi, ok := in.SelectionRange()
	if !ok || lo == hi {
		return false
	}
	runes := []rune(in.ta.Value())
	newVal := string(runes[:lo]) + string(runes[hi:])
	in.ta.SetValue(newVal)
	in.selAnchor = -1
	in.moveCursorAbs(lo)
	return true
}

// InsertText inserts s at the current cursor position via the native
// textarea InsertString (preserves cursor positioning).
func (in *Input) InsertText(s string) {
	if s == "" {
		return
	}
	in.ta.InsertString(s)
}

// ReplaceSelection removes the selected range, inserts s in its place,
// and leaves the cursor just after the inserted text. If there is no
// selection, behaves like InsertText. Returns true if anything was
// replaced (selection existed).
func (in *Input) ReplaceSelection(s string) bool {
	if !in.HasSelection() {
		in.InsertText(s)
		return false
	}
	lo, hi, _ := in.SelectionRange()
	runes := []rune(in.ta.Value())
	newVal := string(runes[:lo]) + s + string(runes[hi:])
	in.ta.SetValue(newVal)
	in.selAnchor = -1
	in.moveCursorAbs(lo + len([]rune(s)))
	return true
}

// DeleteForward deletes the active selection if any, otherwise deletes
// one rune to the right of the cursor.
func (in *Input) DeleteForward() {
	if in.DeleteSelection() {
		return
	}
	pos := in.CursorPos()
	runes := []rune(in.ta.Value())
	if pos >= len(runes) {
		return
	}
	newVal := string(runes[:pos]) + string(runes[pos+1:])
	in.ta.SetValue(newVal)
	in.moveCursorAbs(pos)
}

// DeleteBackward deletes the active selection if any, otherwise deletes
// one rune to the left of the cursor.
func (in *Input) DeleteBackward() {
	if in.DeleteSelection() {
		return
	}
	pos := in.CursorPos()
	if pos <= 0 {
		return
	}
	runes := []rune(in.ta.Value())
	newVal := string(runes[:pos-1]) + string(runes[pos:])
	in.ta.SetValue(newVal)
	in.moveCursorAbs(pos - 1)
}

// Render — direct port of OpenCode editorCmp.View(). The ">" prompt takes

func absolutePos(ta textarea.Model) int {
	info := ta.LineInfo()
	row := ta.Line()
	colInLogicalLine := info.StartColumn + info.ColumnOffset
	if row == 0 {
		return colInLogicalLine
	}
	lines := strings.Split(ta.Value(), "\n")
	pos := 0
	for i := 0; i < row && i < len(lines); i++ {
		pos += len([]rune(lines[i])) + 1 // +1 for '\n'
	}
	return pos + colInLogicalLine
}

// absoluteToRowCol converts an absolute rune index to (logical row, col on row).
// Lines are split by '\n'. Out-of-range pos is clamped to [0, len].
func (in Input) absoluteToRowCol(pos int) (row, col int) {
	runes := []rune(in.ta.Value())
	if pos < 0 {
		pos = 0
	}
	if pos > len(runes) {
		pos = len(runes)
	}
	row = 0
	last := 0
	for i := 0; i < pos; i++ {
		if runes[i] == '\n' {
			row++
			last = i + 1
		}
	}
	col = pos - last
	return row, col
}

// moveCursorAbs positions the textarea cursor at the given absolute rune index.
// Strategy: navigate from the current row to the target row via CursorUp/Down,
// then SetCursor(col) for column-within-line. Clamped to [0, len(value)].
//
// Caveat: ta.Line() and CursorUp/Down count visual rows. When the value is a
// single logical line, this is equivalent to logical rows and works exactly.
// When the value has multiple '\n'-separated logical lines AND at least one
// logical line soft-wraps to multiple visual rows, the visual-vs-logical
// delta diverges and the cursor may land on the wrong row. For chat input
// in Phase 2 this is acceptable because typical lines fit within textarea
// width; revisit if Shift+Enter usage exposes the mismatch in practice.
func (in *Input) moveCursorAbs(pos int) {
	runes := []rune(in.ta.Value())
	if pos < 0 {
		pos = 0
	}
	if pos > len(runes) {
		pos = len(runes)
	}
	row, col := in.absoluteToRowCol(pos)
	currentRow := in.ta.Line()
	delta := row - currentRow
	if delta > 0 {
		for i := 0; i < delta; i++ {
			in.ta.CursorDown()
		}
	} else if delta < 0 {
		for i := 0; i < -delta; i++ {
			in.ta.CursorUp()
		}
	}
	in.ta.SetCursor(col)
}

// currentLineRange returns the absolute [lo, hi) bounds of the logical line
// containing pos. Lines are split by '\n'.
func (in Input) currentLineRange(pos int) (lo, hi int) {
	runes := []rune(in.ta.Value())
	if pos < 0 {
		pos = 0
	}
	if pos > len(runes) {
		pos = len(runes)
	}
	lo = pos
	for lo > 0 && runes[lo-1] != '\n' {
		lo--
	}
	hi = pos
	for hi < len(runes) && runes[hi] != '\n' {
		hi++
	}
	return lo, hi
}

// isWordChar — letter, digit, or underscore.
func isWordChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// clampPos returns pos clamped to [0, max].
func clampPos(pos, max int) int {
	if pos < 0 {
		return 0
	}
	if pos > max {
		return max
	}
	return pos
}

// WordRange returns the absolute [lo, hi) bounds of the word containing
// or adjacent to pos. Word chars are letters, digits, and underscore.
// If pos is on a non-word char with no adjacent word, returns (pos, pos).
func (in Input) WordRange(pos int) (lo, hi int) {
	runes := []rune(in.ta.Value())
	if pos < 0 {
		pos = 0
	}
	if pos > len(runes) {
		pos = len(runes)
	}
	// pos at end-of-value: check the rune to the left.
	if pos == len(runes) {
		if pos > 0 && isWordChar(runes[pos-1]) {
			lo = pos - 1
			for lo > 0 && isWordChar(runes[lo-1]) {
				lo--
			}
			return lo, pos
		}
		return pos, pos
	}
	// pos on a non-word char: try the rune to the left as a fallback.
	if !isWordChar(runes[pos]) {
		if pos > 0 && isWordChar(runes[pos-1]) {
			lo = pos - 1
			for lo > 0 && isWordChar(runes[lo-1]) {
				lo--
			}
			return lo, pos
		}
		return pos, pos
	}
	// pos on a word char: expand both directions.
	lo = pos
	for lo > 0 && isWordChar(runes[lo-1]) {
		lo--
	}
	hi = pos + 1
	for hi < len(runes) && isWordChar(runes[hi]) {
		hi++
	}
	return lo, hi
}

// LineRange returns the absolute [lo, hi) bounds of the logical line
// containing pos. Wrapper around currentLineRange for the public API.
func (in Input) LineRange(pos int) (lo, hi int) {
	return in.currentLineRange(pos)
}

// SelectedText returns the currently selected runes as a string, or ""
// if there is no selection.
func (in Input) SelectedText() string {
	lo, hi, ok := in.SelectionRange()
	if !ok {
		return ""
	}
	runes := []rune(in.ta.Value())
	if hi > len(runes) {
		hi = len(runes)
	}
	if lo < 0 {
		lo = 0
	}
	return string(runes[lo:hi])
}

// Cut returns the selected text and removes it from the value. Returns
// "" if there is no selection.
func (in *Input) Cut() string {
	if !in.HasSelection() {
		return ""
	}
	s := in.SelectedText()
	in.DeleteSelection()
	return s
}

// Paste inserts text at the cursor, replacing any active selection.
// Returns false if text is empty.
func (in *Input) Paste(text string) bool {
	if text == "" {
		return false
	}
	in.ReplaceSelection(text)
	return true
}

// InsertNewline inserts '\n' at the cursor, replacing any active selection.
func (in *Input) InsertNewline() {
	if in.HasSelection() {
		in.ReplaceSelection("\n")
		return
	}
	in.ta.InsertRune('\n')
}

// VisualRow describes one visual row of the input after applying the same
// word-aware wrap bubbles textarea uses internally.
//
//	AbsStart     — absolute rune index in the input value where this row starts.
//	Runes        — slice of the ORIGINAL value runes that make up this row
//	               (no synthetic trailing spaces — caller picks padding policy).
//	EndOfLogical — true when this row is the last visual row of its '\n'-

func (in *Input) CollapseSelectionToStart() bool {
	lo, _, ok := in.SelectionRange()
	if !ok {
		return false
	}
	in.ClearSelection()
	in.moveCursorAbs(lo)
	return true
}

// CollapseSelectionToEnd clears the selection and places the cursor at the
// selection end (desktop-editor Right after Select-All).
func (in *Input) CollapseSelectionToEnd() bool {
	_, hi, ok := in.SelectionRange()
	if !ok {
		return false
	}
	in.ClearSelection()
	in.moveCursorAbs(hi)
	return true
}
