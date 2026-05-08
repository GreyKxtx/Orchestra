package view

import (
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/theme"
)

// Input is the bottom editor — direct port of OpenCode's editor.View().
//   style.Render(">") + textarea.View()
type Input struct {
	ta    textarea.Model
	width int
}

// NewInput creates a textarea styled like OpenCode's CreateTextArea.
func NewInput(width int) Input {
	t := theme.CurrentTheme()
	bgColor := t.Background()
	textColor := t.Text()
	textMutedColor := t.TextMuted()

	base := lipgloss.NewStyle()

	ta := textarea.New()
	ta.BlurredStyle.Base = base.Background(bgColor).Foreground(textColor)
	ta.BlurredStyle.CursorLine = base.Background(bgColor)
	ta.BlurredStyle.Placeholder = base.Background(bgColor).Foreground(textMutedColor)
	ta.BlurredStyle.Text = base.Background(bgColor).Foreground(textColor)
	ta.BlurredStyle.EndOfBuffer = base.Background(bgColor)

	ta.FocusedStyle.Base = base.Background(bgColor).Foreground(textColor)
	ta.FocusedStyle.CursorLine = base.Background(bgColor)
	ta.FocusedStyle.Placeholder = base.Background(bgColor).Foreground(textMutedColor)
	ta.FocusedStyle.Text = base.Background(bgColor).Foreground(textColor)
	ta.FocusedStyle.EndOfBuffer = base.Background(bgColor)

	ta.Placeholder = "Спроси Orchestra…"
	ta.Prompt = " "
	ta.ShowLineNumbers = false
	ta.CharLimit = -1
	ta.SetWidth(width - 2) // " >" prompt occupies 2 cols
	ta.SetHeight(1)

	ta.Focus()
	return Input{ta: ta, width: width}
}

// SetMode is a no-op kept for API compatibility (mode shown elsewhere now).
func (in *Input) SetMode(mode string) {}

// SetSize resizes the textarea.
func (in *Input) SetSize(width int) {
	in.width = width
	in.ta.SetWidth(width - 2)
}

// Value returns the current text.
func (in Input) Value() string { return in.ta.Value() }

// Reset clears the input.
func (in *Input) Reset() { in.ta.Reset() }

// SetValue replaces the current text.
func (in *Input) SetValue(s string) { in.ta.SetValue(s) }

// Inner returns the underlying textarea so app.go can route key events.
func (in *Input) Inner() *textarea.Model { return &in.ta }

// Render — direct port of OpenCode editorCmp.View().
func (in Input) Render() string {
	t := theme.CurrentTheme()
	style := lipgloss.NewStyle().
		Padding(0, 0, 0, 1).
		Bold(true).
		Foreground(t.Primary())
	return lipgloss.JoinHorizontal(lipgloss.Top, style.Render(">"), in.ta.View())
}
