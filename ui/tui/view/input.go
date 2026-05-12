package view

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/theme"
)

// Input is the bottom editor — direct port of OpenCode's editor.View().
//   style.Render(">") + textarea.View()
type Input struct {
	ta        textarea.Model
	width     int
	taWidth   int    // outer width last passed to SetTextareaWidth (≠ in.ta.Width())
	mode      string // current agent mode; drives prompt color
	selAnchor int    // -1 = no selection; ≥0 = rune index where selection started
	// Mouse drag caret: when mouseCaretActive, renders at mouseCaret instead of ta cursor.
	mouseCaret       int
	mouseCaretActive bool
}

// NewInput creates a textarea styled like OpenCode's CreateTextArea.
// The textarea uses BackgroundSecondary so the grey input box stands out
// against the main BG (#0d0d0d vs #1a1a1a) — same visual as OpenCode.
func NewInput(width int) Input {
	t := theme.CurrentTheme()
	bgColor := t.BackgroundSecondary()
	textColor := t.Text()
	textMutedColor := t.TextMuted()

	base := lipgloss.NewStyle()

	ta := textarea.New()
	bgFill := base.Background(bgColor)
	ta.BlurredStyle.Base = bgFill.Foreground(textColor)
	ta.BlurredStyle.CursorLine = bgFill
	ta.BlurredStyle.CursorLineNumber = bgFill
	ta.BlurredStyle.LineNumber = bgFill
	ta.BlurredStyle.Placeholder = bgFill.Foreground(textMutedColor)
	ta.BlurredStyle.Prompt = bgFill
	ta.BlurredStyle.Text = bgFill.Foreground(textColor)
	ta.BlurredStyle.EndOfBuffer = bgFill

	ta.FocusedStyle.Base = bgFill.Foreground(textColor)
	ta.FocusedStyle.CursorLine = bgFill
	ta.FocusedStyle.CursorLineNumber = bgFill
	ta.FocusedStyle.LineNumber = bgFill
	ta.FocusedStyle.Placeholder = bgFill.Foreground(textMutedColor)
	ta.FocusedStyle.Prompt = bgFill
	ta.FocusedStyle.Text = bgFill.Foreground(textColor)
	ta.FocusedStyle.EndOfBuffer = bgFill

	// Cursor — without explicit styling it renders as terminal-default reverse
	// video (black bg) which leaks through our grey input box. Set both the
	// "on" Style and the "under cursor" TextStyle to use our bg.
	ta.Cursor.Style = lipgloss.NewStyle().Background(t.Primary()).Foreground(bgColor)
	ta.Cursor.TextStyle = lipgloss.NewStyle().Background(bgColor).Foreground(textColor)

	ta.Placeholder = "Спроси Orchestra…"
	ta.Prompt = " "
	ta.ShowLineNumbers = false
	ta.CharLimit = -1
	ta.SetWidth(width - 2) // " >" prompt occupies 2 cols
	ta.SetHeight(1)

	// Extend word-jump bindings so Ctrl+Left/Right also trigger word
	// navigation (bubbles defaults only bind Alt+Left/B and Alt+Right/F,
	// which doesn't match the desktop-editor convention users expect).
	ta.KeyMap.WordBackward = key.NewBinding(key.WithKeys("alt+left", "alt+b", "ctrl+left"))
	ta.KeyMap.WordForward = key.NewBinding(key.WithKeys("alt+right", "alt+f", "ctrl+right"))

	ta.Focus()
	return Input{ta: ta, width: width, taWidth: width - 2, selAnchor: -1}
}

// SetMode stores the agent mode so the prompt prefix can be colored to match
// the mode label elsewhere in the UI.
func (in *Input) SetMode(mode string) { in.mode = mode }

// SetSize resizes the textarea.
func (in *Input) SetSize(width int) {
	in.width = width
	in.taWidth = width - 2
	in.ta.SetWidth(width - 2)
}

// SetTextareaWidth lets external renderers (e.g. welcome view) set the
// textarea width without touching the input's own width tracking.
// We remember the EXACT width we were asked for so TextareaWidth can
// return it later — bubbles' internal m.width is post-decremented by
// the prompt width, which would otherwise cause save/restore dances
// (renderInputBox does one) to drift the width by 1 cell per render.
func (in *Input) SetTextareaWidth(w int) {
	in.taWidth = w
	in.ta.SetWidth(w)
}

// TextareaWidth returns the width that was last passed to SetTextareaWidth
// (i.e. the OUTER width including prompt), suitable for restoration.
func (in Input) TextareaWidth() int {
	if in.taWidth > 0 {
		return in.taWidth
	}
	return in.ta.Width()
}

// TextareaView renders just the underlying textarea (no "> " prompt).
// Used by the welcome view which renders the input inside a styled box.
func (in Input) TextareaView() string { return in.ta.View() }

// Value returns the current text.
func (in Input) Value() string { return in.ta.Value() }

// Reset clears the input.
func (in *Input) Reset() { in.ta.Reset() }

// SetValue replaces the current text.
func (in *Input) SetValue(s string) { in.ta.SetValue(s) }

// Inner returns the underlying textarea so app.go can route key events.
func (in *Input) Inner() *textarea.Model { return &in.ta }

// CursorPos returns the absolute rune index of the textarea cursor across all lines.
func (in Input) CursorPos() int { return absolutePos(in.ta) }

// HasSelection reports whether a selection range is active.
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

// MoveCursorVisualUp moves the cursor up by one visual row, preserving
// the visual column. `desiredCol` lets the caller pass a sticky "desired
// column" across a sequence of Up/Down presses so the cursor doesn't
// permanently lose its column when stepping through a short row.
// Pass desiredCol < 0 to capture the current column. Returns the
// effective column used for the move (caller persists it for the next
// step). No-op if already on the top visual row.
func (in *Input) MoveCursorVisualUp(width, desiredCol int) int {
	if width < 1 {
		width = 1
	}
	val := in.ta.Value()
	pos := in.CursorPos()
	vRow, vCol := absToVisualRowCol(val, pos, width)
	targetCol := vCol
	if desiredCol >= 0 {
		targetCol = desiredCol
	}
	if vRow == 0 {
		return targetCol
	}
	newPos := visualRowColToAbs(val, vRow-1, targetCol, width)
	in.moveCursorAbs(newPos)
	return targetCol
}

// MoveCursorVisualDown moves the cursor down by one visual row. See
// MoveCursorVisualUp for desiredCol semantics.
func (in *Input) MoveCursorVisualDown(width, desiredCol int) int {
	if width < 1 {
		width = 1
	}
	val := in.ta.Value()
	pos := in.CursorPos()
	vRow, vCol := absToVisualRowCol(val, pos, width)
	targetCol := vCol
	if desiredCol >= 0 {
		targetCol = desiredCol
	}
	totalRows := in.VisualLineCount(width)
	if vRow >= totalRows-1 {
		return targetCol
	}
	newPos := visualRowColToAbs(val, vRow+1, targetCol, width)
	in.moveCursorAbs(newPos)
	return targetCol
}

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
// the current agent-mode color so the prompt, the welcome input bar, and
// the mode label all share one accent across views.
func (in Input) Render() string {
	style := lipgloss.NewStyle().
		Padding(0, 0, 0, 1).
		Bold(true).
		Foreground(ModeColor(in.mode))
	return lipgloss.JoinHorizontal(lipgloss.Top, style.Render(">"), in.ta.View())
}

// WelcomeRender renders the input rows for the welcome view and chat box
// ourselves, bypassing bubbles textarea.View(). Renders each logical line
// (split by '\n') on its own row with consistent overlay (cursor + selection).
//
//	width    — target visible width of each row (matches box content area)
//	blinkOn  — whether the cursor is currently visible (animation)
func (in Input) WelcomeRender(width int, blinkOn bool) string {
	t := theme.CurrentTheme()
	bg := t.BackgroundSecondary()

	bgStyle := lipgloss.NewStyle().Background(bg)
	textStyle := bgStyle.Foreground(t.Text())
	mutedStyle := bgStyle.Foreground(t.TextMuted())
	cursorStyle := lipgloss.NewStyle().Background(t.Primary()).Foreground(t.Background())
	selStyle := lipgloss.NewStyle().Background(t.BorderNormal()).Foreground(t.Text())
	bar := lipgloss.NewStyle().Background(bg).Foreground(t.Primary()).Bold(true).Render("│")

	val := in.ta.Value()
	totalRunes := len([]rune(val))

	// Empty input — placeholder is always visible; only the bar cursor in
	// front of it blinks (replaced with a same-width space when off) so the
	// placeholder text itself doesn't flicker on/off with each frame.
	if val == "" {
		ph := mutedStyle.Render("Спроси Orchestra…")
		if blinkOn {
			return padLine(bar+ph, width, bgStyle)
		}
		return padLine(bgStyle.Render(" ")+ph, width, bgStyle)
	}

	// Resolve cursor absolute position (mouse-caret override during drag).
	var cursorPos int
	if in.mouseCaretActive {
		cursorPos = clampPos(in.mouseCaret, totalRunes)
	} else {
		cursorPos = clampPos(absolutePos(in.ta), totalRunes)
	}
	selMin, selMax, hasSel := in.SelectionRange()

	// Build visual chunks: each logical line is further split into rows
	// of up to `width` runes (soft-wrap). Track the absolute rune offset
	// of each chunk's first rune and whether it's the LAST chunk of its
	// logical line (so the bar cursor at end-of-chunk is drawn only on
	// real line endings, never on a wrap continuation).
	type chunk struct {
		absStart       int
		runes          []rune
		endOfLogical   bool
	}
	wrapW := width
	if wrapW < 1 {
		wrapW = 1
	}
	logical := strings.Split(val, "\n")
	chunks := make([]chunk, 0, len(logical))
	absOffset := 0
	for _, line := range logical {
		runes := []rune(line)
		if len(runes) == 0 {
			chunks = append(chunks, chunk{absStart: absOffset, runes: nil, endOfLogical: true})
		} else {
			for i := 0; i < len(runes); i += wrapW {
				end := i + wrapW
				if end > len(runes) {
					end = len(runes)
				}
				chunks = append(chunks, chunk{
					absStart:     absOffset + i,
					runes:        runes[i:end],
					endOfLogical: end == len(runes),
				})
			}
		}
		absOffset += len(runes) + 1 // +1 for '\n'
	}

	rendered := make([]string, 0, len(chunks))
	for _, c := range chunks {
		var b strings.Builder
		b.Grow(len(c.runes) * 20)
		for i, r := range c.runes {
			absIdx := c.absStart + i
			ch := string(r)
			isCursor := blinkOn && absIdx == cursorPos
			isSelected := hasSel && absIdx >= selMin && absIdx < selMax
			switch {
			case isCursor:
				b.WriteString(cursorStyle.Render(ch))
			case isSelected:
				b.WriteString(selStyle.Render(ch))
			default:
				b.WriteString(textStyle.Render(ch))
			}
		}
		// Bar cursor at end of THIS chunk iff cursor sits there AND
		// this chunk is the end of a logical line — otherwise the
		// position equals the start of the next chunk (continuation
		// wrap), where the block cursor will be drawn instead.
		endOfChunkAbs := c.absStart + len(c.runes)
		if blinkOn && cursorPos == endOfChunkAbs && c.endOfLogical {
			b.WriteString(bar)
		}
		rendered = append(rendered, padLine(b.String(), width, bgStyle))
	}

	return strings.Join(rendered, "\n")
}

// padLine pads s to exactly width visible cells using bgStyle-filled spaces.
func padLine(s string, width int, bgStyle lipgloss.Style) string {
	if diff := width - lipgloss.Width(s); diff > 0 {
		s += bgStyle.Render(strings.Repeat(" ", diff))
	}
	return s
}

// absolutePos computes the absolute rune index of the textarea cursor,
// accounting for both logical lines (separated by '\n') and soft-wrap
// (a single logical line that exceeds textarea width and wraps to
// multiple visual rows).
//
// LineInfo.StartColumn is the column-within-logical-line where the
// current wrapped visual row starts; LineInfo.CharOffset is the visual
// column within that wrapped row. Their sum is the column within the
// current logical line — correct regardless of soft-wrap.
func absolutePos(ta textarea.Model) int {
	info := ta.LineInfo()
	row := ta.Line()
	colInLogicalLine := info.StartColumn + info.CharOffset
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

// absToVisualRowCol converts an absolute rune index into visual coordinates
// (row, col) given soft-wrap width. Logical lines are separated by '\n';
// each logical line wraps to ceil(len/width) visual rows (minimum 1).
func absToVisualRowCol(val string, absPos, width int) (vRow, vCol int) {
	if width < 1 {
		width = 1
	}
	if absPos < 0 {
		absPos = 0
	}
	offset := 0
	for _, line := range strings.Split(val, "\n") {
		rl := len([]rune(line))
		if absPos <= offset+rl {
			posInLine := absPos - offset
			// Edge case: cursor exactly at end of line on a wrap-row
			// boundary — visually it sits at the END of the previous
			// visual row (col = width), not at the start of a phantom
			// next visual row (col = 0).
			if posInLine == rl && rl > 0 && posInLine%width == 0 {
				vRow += (posInLine / width) - 1
				vCol = width
				return
			}
			vRow += posInLine / width
			vCol = posInLine % width
			return
		}
		rowsInLine := rl / width
		if rl == 0 || rl%width != 0 {
			rowsInLine++
		}
		vRow += rowsInLine
		offset += rl + 1
	}
	return
}

// visualRowColToAbs converts visual coordinates back to an absolute rune
// index, clamping vCol to the actual line length on the target row.
func visualRowColToAbs(val string, vRow, vCol, width int) int {
	if width < 1 {
		width = 1
	}
	if vRow < 0 {
		vRow = 0
	}
	if vCol < 0 {
		vCol = 0
	}
	abs := 0
	rowsLeft := vRow
	for _, line := range strings.Split(val, "\n") {
		rl := len([]rune(line))
		rowsInLine := rl / width
		if rl == 0 || rl%width != 0 {
			rowsInLine++
		}
		if rowsLeft < rowsInLine {
			// Target row sits inside this logical line.
			lineRowStart := rowsLeft * width
			colInLine := lineRowStart + vCol
			lineRowEnd := lineRowStart + width
			if lineRowEnd > rl {
				lineRowEnd = rl
			}
			if colInLine > lineRowEnd {
				colInLine = lineRowEnd
			}
			return abs + colInLine
		}
		rowsLeft -= rowsInLine
		abs += rl + 1
	}
	runes := []rune(val)
	return len(runes)
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

// VisualLineCount returns the total number of visual rows the value
// would occupy when soft-wrapped to the given width. Each '\n' starts
// a new logical line; each logical line wraps to ceil(len/width) rows
// (minimum 1 even for an empty line).
func (in Input) VisualLineCount(width int) int {
	if width < 1 {
		width = 1
	}
	val := in.ta.Value()
	if val == "" {
		return 1
	}
	total := 0
	for _, line := range strings.Split(val, "\n") {
		rl := len([]rune(line))
		if rl == 0 {
			total++
			continue
		}
		total += (rl + width - 1) / width
	}
	if total < 1 {
		return 1
	}
	return total
}

// WrapWidth returns the outer width last set via SetTextareaWidth — the
// same value renderInputBox uses as contentW. Prefer this over
// in.ta.Width() / Inner().Width(): bubbles' SetWidth post-decrements
// m.width by the prompt width, so a save+restore dance drifts the value
// by 1 cell per render.
func (in Input) WrapWidth() int {
	if in.taWidth > 0 {
		return in.taWidth
	}
	return in.ta.Width()
}

// SyncHeight caps the textarea height to the visual line count given
// the current textarea wrap width, clamped to [1, max]. Call after any
// value mutation so the visible rows match the actual content (including
// soft-wrap, not just '\n' splits).
func (in *Input) SyncHeight(max int) {
	w := in.WrapWidth()
	if w < 1 {
		w = 80
	}
	h := in.VisualLineCount(w)
	if h < 1 {
		h = 1
	}
	if max < 1 {
		max = 1
	}
	if h > max {
		h = max
	}
	in.ta.SetHeight(h)
}
