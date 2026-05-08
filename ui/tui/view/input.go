package view

import (
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/theme"
)

// Input is the styled editor at the bottom of the screen.
// Layout (OpenCode-style, top-down):
//   row 1: " > " prompt + textarea (1 line, full-width BackgroundSecondary)
//   row 2: " ⏵ <mode>  ·  tab agent  ·  ctrl+k commands  ·  ctrl+o model "
type Input struct {
	ta    textarea.Model
	mode  string
	width int
}

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
	ta.SetHeight(1)
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
	ta.FocusedStyle.EndOfBuffer = base
	ta.BlurredStyle.Base = base
	ta.BlurredStyle.CursorLine = base
	ta.BlurredStyle.Text = base
	ta.BlurredStyle.Placeholder = muted
	ta.BlurredStyle.Prompt = prompt
	ta.BlurredStyle.EndOfBuffer = base

	return Input{ta: ta, width: width}
}

// SetMode sets the agent mode label shown in the info bar.
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

// Render draws the textarea + a contextual info bar below it.
func (in Input) Render() string {
	t := theme.CurrentTheme()
	bg := t.BackgroundSecondary()

	// Info bar below the textarea — full width, BackgroundSecondary fill.
	infoBar := lipgloss.NewStyle().
		Background(bg).
		Foreground(t.TextMuted()).
		Width(in.width).
		Padding(0, 1).
		Render(in.buildInfo())

	return lipgloss.JoinVertical(lipgloss.Left, in.ta.View(), infoBar)
}

// buildInfo composes the mode label + keyboard hints shown under the input.
func (in Input) buildInfo() string {
	t := theme.CurrentTheme()
	bg := t.BackgroundSecondary()

	modeStyle := lipgloss.NewStyle().Background(bg).Foreground(t.Primary()).Bold(true)
	mutedStyle := lipgloss.NewStyle().Background(bg).Foreground(t.TextMuted())
	dot := mutedStyle.Render(" · ")

	parts := []string{}
	if in.mode != "" {
		parts = append(parts, modeStyle.Render("⏵ "+in.mode))
	}
	parts = append(parts,
		mutedStyle.Render("tab agent"),
		mutedStyle.Render("ctrl+k commands"),
		mutedStyle.Render("ctrl+o model"),
	)

	out := ""
	for i, p := range parts {
		if i > 0 {
			out += dot
		}
		out += p
	}
	return out
}
