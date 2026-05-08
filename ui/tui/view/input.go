package view

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/theme"
)

// Input is the styled editor at the bottom of the screen.
// It renders a labeled separator (with the current mode) above a
// background-highlighted textarea with a primary-colored ">" prompt.
type Input struct {
	ta    textarea.Model
	mode  string
	width int
}

// NewInput creates a sized input with theme-aware background styling.
func NewInput(width int) Input {
	t := theme.CurrentTheme()
	bg := t.BackgroundSecondary()
	fg := t.Text()
	fgMuted := t.TextMuted()

	ta := textarea.New()
	ta.Placeholder = "Спроси Orchestra…"
	ta.SetWidth(width - 4) // leave 4 cols for " > " prompt
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.Focus()

	base := lipgloss.NewStyle().Background(bg).Foreground(fg)
	muted := lipgloss.NewStyle().Background(bg).Foreground(fgMuted)

	ta.FocusedStyle.Base = base
	ta.FocusedStyle.CursorLine = base
	ta.FocusedStyle.Text = base
	ta.FocusedStyle.Placeholder = muted
	ta.BlurredStyle.Base = base
	ta.BlurredStyle.CursorLine = base
	ta.BlurredStyle.Text = base
	ta.BlurredStyle.Placeholder = muted

	return Input{ta: ta, width: width}
}

// SetMode sets the current agent mode label shown in the separator bar.
func (in *Input) SetMode(mode string) { in.mode = mode }

// SetSize resizes the input.
func (in *Input) SetSize(width int) {
	in.width = width
	in.ta.SetWidth(width - 4)
}

// Value returns the current text.
func (in Input) Value() string { return in.ta.Value() }

// Reset clears the input.
func (in *Input) Reset() { in.ta.Reset() }

// SetValue replaces the current text.
func (in *Input) SetValue(s string) { in.ta.SetValue(s) }

// Inner returns the underlying textarea so app.go can route key events.
func (in *Input) Inner() *textarea.Model { return &in.ta }

// Render draws the separator bar + highlighted input area.
func (in Input) Render() string {
	t := theme.CurrentTheme()
	bg := t.BackgroundSecondary()

	sepColor := t.TextMuted()
	modeColor := t.Primary()

	// ── mode ──────────────── separator bar (1 row)
	var sepBar string
	{
		sepStyle := lipgloss.NewStyle().Background(bg).Foreground(sepColor)
		if in.mode != "" {
			modeStyle := lipgloss.NewStyle().Background(bg).Foreground(modeColor).Bold(true)
			label := " " + in.mode + " "
			labelW := lipgloss.Width(label)
			// "── " + label + "─────..."
			right := in.width - 3 - labelW
			if right < 0 {
				right = 0
			}
			sepBar = sepStyle.Render("── ") +
				modeStyle.Render(label) +
				sepStyle.Render(strings.Repeat("─", right))
		} else {
			sepBar = sepStyle.Render(strings.Repeat("─", in.width))
		}
	}

	// " > " prompt + textarea (3 rows)
	promptStyle := lipgloss.NewStyle().
		Background(bg).
		Foreground(modeColor).
		Bold(true)
	prompt := promptStyle.Render(" > ")

	taArea := lipgloss.JoinHorizontal(lipgloss.Top, prompt, in.ta.View())

	// Fill remaining width with background
	areaStyle := lipgloss.NewStyle().Background(bg).Width(in.width)

	return lipgloss.JoinVertical(lipgloss.Left,
		sepBar,
		areaStyle.Render(taArea),
	)
}
