package view

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"
	runewidth "github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"

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

	// Build visual chunks using the SAME word-aware wrap as bubbles' internal
	// textarea (see wordWrap below). Char-at-wrapW chunking disagrees with
	// bubbles whenever the text contains spaces — that mismatch shifts the
	// rendered cursor away from where bubbles' CursorUp/CursorDown actually
	// moved it. `width` is still used by padLine to pad each rendered row to
	// the outer box width.
	wrapW := in.WrapWidth()
	if wrapW < 1 {
		wrapW = width
	}
	if wrapW < 1 {
		wrapW = 1
	}
	chunks := in.VisualRows(wrapW)

	rendered := make([]string, 0, len(chunks))
	for _, c := range chunks {
		var b strings.Builder
		b.Grow(len(c.Runes) * 20)
		for i, r := range c.Runes {
			absIdx := c.AbsStart + i
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
		endOfChunkAbs := c.AbsStart + len(c.Runes)
		if blinkOn && cursorPos == endOfChunkAbs && c.EndOfLogical {
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
// accounting for both logical lines (separated by '\n') and soft-wrap.
//
// StartColumn is the rune index within the current logical line where
// the current wrapped visual row starts; ColumnOffset is the rune offset
// from StartColumn to the cursor. Both are in RUNE units. (CharOffset
// would be visual-cell units via uniseg.StringWidth, wrong for adding.)
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
//	               delimited logical line (or end of value).
type VisualRow struct {
	AbsStart     int
	Runes        []rune
	EndOfLogical bool
}

// VisualRows computes the word-wrapped breakdown of in.Value() at width w
// using the exact same algorithm as bubbles textarea's wrap (so our row
// boundaries align with bubbles' CursorUp/CursorDown / LineInfo). Result
// always has at least one row.
func (in Input) VisualRows(w int) []VisualRow {
	if w < 1 {
		w = 1
	}
	val := in.ta.Value()
	if val == "" {
		return []VisualRow{{AbsStart: 0, Runes: nil, EndOfLogical: true}}
	}
	var rows []VisualRow
	absOffset := 0
	for _, line := range strings.Split(val, "\n") {
		runes := []rune(line)
		if len(runes) == 0 {
			rows = append(rows, VisualRow{AbsStart: absOffset, Runes: nil, EndOfLogical: true})
		} else {
			starts := wordWrapStarts(runes, w)
			for i, s := range starts {
				end := len(runes)
				if i+1 < len(starts) {
					end = starts[i+1]
				}
				rows = append(rows, VisualRow{
					AbsStart:     absOffset + s,
					Runes:        runes[s:end],
					EndOfLogical: i == len(starts)-1,
				})
			}
		}
		absOffset += len(runes) + 1
	}
	if len(rows) == 0 {
		rows = append(rows, VisualRow{AbsStart: 0, Runes: nil, EndOfLogical: true})
	}
	return rows
}

// VisualLineCount returns the total number of visual rows the value occupies
// when soft-wrapped to the given width using bubbles' word-aware algorithm.
func (in Input) VisualLineCount(width int) int {
	return len(in.VisualRows(width))
}

// wordWrap is a verbatim port of bubbles textarea's internal wrap(): it
// produces the visual grid of a single logical line with word-aware breaks.
// Same algorithm → same row boundaries → cursor/selection overlays align
// with bubbles' own LineInfo / CursorUp / CursorDown.
//
// The grid may contain ONE synthetic trailing space at the very end of the
// last row (bubbles adds it for consistent cursor-at-end behaviour). All
// other runes in the grid appear in the same order as `runes` — see
// wordWrapStarts for how we recover input-rune offsets.
func wordWrap(runes []rune, width int) [][]rune {
	if width < 1 {
		width = 1
	}
	var (
		lines  = [][]rune{{}}
		word   = []rune{}
		row    int
		spaces int
	)
	for _, r := range runes {
		if unicode.IsSpace(r) {
			spaces++
		} else {
			word = append(word, r)
		}
		if spaces > 0 {
			if uniseg.StringWidth(string(lines[row]))+uniseg.StringWidth(string(word))+spaces > width {
				row++
				lines = append(lines, []rune{})
				lines[row] = append(lines[row], word...)
				lines[row] = append(lines[row], []rune(strings.Repeat(" ", spaces))...)
			} else {
				lines[row] = append(lines[row], word...)
				lines[row] = append(lines[row], []rune(strings.Repeat(" ", spaces))...)
			}
			spaces = 0
			word = nil
		} else {
			if len(word) == 0 {
				continue
			}
			lastCharLen := runewidth.RuneWidth(word[len(word)-1])
			if uniseg.StringWidth(string(word))+lastCharLen > width {
				if len(lines[row]) > 0 {
					row++
					lines = append(lines, []rune{})
				}
				lines[row] = append(lines[row], word...)
				word = nil
			}
		}
	}
	if uniseg.StringWidth(string(lines[row]))+uniseg.StringWidth(string(word))+spaces >= width {
		lines = append(lines, []rune{})
		lines[row+1] = append(lines[row+1], word...)
		spaces++
		lines[row+1] = append(lines[row+1], []rune(strings.Repeat(" ", spaces))...)
	} else {
		lines[row] = append(lines[row], word...)
		spaces++
		lines[row] = append(lines[row], []rune(strings.Repeat(" ", spaces))...)
	}
	return lines
}

// wordWrapStarts returns the rune indices (within `runes`) where each visual
// row begins after applying word-aware wrap at `width`. Always returns at
// least [0]. Built from wordWrap's grid: every non-last row contains exactly
// its input runes (no synthetic), so len(grid[i]) for i<N-1 is the count of
// input runes consumed by row i. The last row may have +1 synthetic trailing
// space — irrelevant for start offsets but caller must clamp slice bounds.
func wordWrapStarts(runes []rune, width int) []int {
	if len(runes) == 0 {
		return []int{0}
	}
	grid := wordWrap(runes, width)
	starts := make([]int, 0, len(grid))
	starts = append(starts, 0)
	consumed := 0
	for i := 0; i < len(grid)-1; i++ {
		consumed += len(grid[i])
		starts = append(starts, consumed)
	}
	return starts
}

// WrapWidth returns the wrap width bubbles textarea actually uses
// internally (= outer width minus prompt width minus borders). Use
// this for any wrap-aware computation (VisualLineCount, WelcomeRender
// chunking) so our row breakdown matches bubbles' own — otherwise the
// rendered cursor and the cursor bubbles moves with KeyUp/KeyDown
// disagree about which visual row the cursor sits on.
func (in Input) WrapWidth() int {
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
