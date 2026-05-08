package view

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/theme"
)

// Input is the styled editor at the bottom of the screen.
// Mirrors OpenCode's approach: the "> " prompt lives inside the textarea
// so the background color covers the full width without gaps.
type Input struct {
	ta    textarea.Model
	mode  string
	width int
}

// promptStr is the fixed prompt prepended to every textarea line.
const promptStr = " > "

// NewInput creates a sized input with theme-aware background and prompt styling.
func NewInput(width int) Input {
	t := theme.CurrentTheme()
	bg := t.BackgroundSecondary()
	fg := t.Text()
	fgMuted := t.TextMuted()
	primary := t.Primary()

	ta := textarea.New()
	ta.Placeholder = "Спроси Orchestra…"
	ta.SetWidth(width - len(promptStr))
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	ta.Prompt = promptStr
	ta.Focus()

	base := lipgloss.NewStyle().Background(bg).Foreground(fg)
	muted := lipgloss.NewStyle().Background(bg).Foreground(fgMuted)
	prompt := lipgloss.NewStyle().Background(bg).Foreground(primary).Bold(true)

	ta.FocusedStyle.Base = base
	ta.FocusedStyle.CursorLine = base
	ta.FocusedStyle.Text = base
	ta.FocusedStyle.Placeholder = muted
	ta.FocusedStyle.Prompt = prompt
	ta.BlurredStyle.Base = base
	ta.BlurredStyle.CursorLine = base
	ta.BlurredStyle.Text = base
	ta.BlurredStyle.Placeholder = muted
	ta.BlurredStyle.Prompt = prompt

	return Input{ta: ta, width: width}
}

// SetMode sets the agent mode label shown in the separator bar.
func (in *Input) SetMode(mode string) { in.mode = mode }

// SetSize resizes the input.
func (in *Input) SetSize(width int) {
	in.width = width
	in.ta.SetWidth(width - len(promptStr))
}

// Value returns the current text.
func (in Input) Value() string { return in.ta.Value() }

// Reset clears the input.
func (in *Input) Reset() { in.ta.Reset() }

// SetValue replaces the current text.
func (in *Input) SetValue(s string) { in.ta.SetValue(s) }

// Inner returns the underlying textarea so app.go can route key events.
func (in *Input) Inner() *textarea.Model { return &in.ta }

// Render draws a separator bar (with optional mode label) above the textarea.
// The full width is always covered by BackgroundSecondary.
func (in Input) Render() string {
	t := theme.CurrentTheme()
	bg := t.BackgroundSecondary()
	sepColor := t.TextMuted()
	modeColor := t.Primary()

	sepStyle := lipgloss.NewStyle().Background(bg).Foreground(sepColor)
	modeStyle := lipgloss.NewStyle().Background(bg).Foreground(modeColor).Bold(true)

	// Build separator: "──" + " mode " + "─────…"
	var sepBar string
	if in.mode != "" {
		left := sepStyle.Render("──")
		label := modeStyle.Render(" " + in.mode + " ")
		remaining := in.width - lipgloss.Width(left) - lipgloss.Width(label)
		if remaining < 0 {
			remaining = 0
		}
		sepBar = left + label + sepStyle.Render(strings.Repeat("─", remaining))
	} else {
		sepBar = sepStyle.Render(strings.Repeat("─", in.width))
	}

	// The textarea already fills (width - len(promptStr)) with background;
	// the prompt " > " adds the remaining len(promptStr) chars — also with bg.
	return lipgloss.JoinVertical(lipgloss.Left, sepBar, in.ta.View())
}
