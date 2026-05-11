package view

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/theme"
)

// Input is the bottom editor — direct port of OpenCode's editor.View().
//   style.Render(">") + textarea.View()
type Input struct {
	ta    textarea.Model
	width int
	mode  string // current agent mode; drives prompt color
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

	ta.Focus()
	return Input{ta: ta, width: width}
}

// SetMode stores the agent mode so the prompt prefix can be colored to match
// the mode label elsewhere in the UI.
func (in *Input) SetMode(mode string) { in.mode = mode }

// SetSize resizes the textarea.
func (in *Input) SetSize(width int) {
	in.width = width
	in.ta.SetWidth(width - 2)
}

// SetTextareaWidth lets external renderers (e.g. welcome view) set the
// textarea width without touching the input's own width tracking.
func (in *Input) SetTextareaWidth(w int) { in.ta.SetWidth(w) }

// TextareaWidth returns the current textarea width.
func (in Input) TextareaWidth() int { return in.ta.Width() }

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

// WelcomeRender renders the input row for the welcome view ourselves,
// bypassing bubbles textarea.View() — its placeholder padding doesn't
// always carry our bg, leaving black gaps. By rendering manually we
// control every cell with explicit lipgloss styles.
//
//	width    — target visible width of the row (matches box content area)
//	blinkOn  — whether the cursor is currently visible (animation)
func (in Input) WelcomeRender(width int, blinkOn bool) string {
	t := theme.CurrentTheme()
	bg := t.BackgroundSecondary()

	bgStyle := lipgloss.NewStyle().Background(bg)
	textStyle := bgStyle.Foreground(t.Text())
	mutedStyle := bgStyle.Foreground(t.TextMuted())
	cursorStyle := lipgloss.NewStyle().Background(t.Primary()).Foreground(t.Background())
	bar := lipgloss.NewStyle().Background(bg).Foreground(t.Primary()).Bold(true).Render("│")

	val := in.ta.Value()
	runes := []rune(val)

	if val == "" {
		if blinkOn {
			return padLine(bar, width, bgStyle)
		}
		return padLine(mutedStyle.Render("Спроси Orchestra…"), width, bgStyle)
	}

	info := in.ta.LineInfo()
	pos := clampPos(info.CharOffset, len(runes))

	var b strings.Builder
	b.Grow(len(runes) * 20)
	for i, r := range runes {
		ch := string(r)
		if blinkOn && i == pos {
			b.WriteString(cursorStyle.Render(ch))
		} else {
			b.WriteString(textStyle.Render(ch))
		}
	}
	if blinkOn && pos == len(runes) {
		b.WriteString(bar)
	}

	return padLine(b.String(), width, bgStyle)
}

// padLine pads s to exactly width visible cells using bgStyle-filled spaces.
func padLine(s string, width int, bgStyle lipgloss.Style) string {
	if diff := width - lipgloss.Width(s); diff > 0 {
		s += bgStyle.Render(strings.Repeat(" ", diff))
	}
	return s
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
