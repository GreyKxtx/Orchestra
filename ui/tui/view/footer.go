package view

import "github.com/charmbracelet/lipgloss"

// Footer is the bottom-of-screen hints line.
type Footer struct {
	width int
	hints string
}

// SetSize updates the footer's known width.
func (f *Footer) SetSize(width int) {
	f.width = width
}

// SetHints overrides the default hints text.
func (f *Footer) SetHints(h string) { f.hints = h }

// Render returns the styled footer hints.
func (f Footer) Render() string {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		Width(f.width).
		Padding(0, 1)
	h := f.hints
	if h == "" {
		h = "↑↓ history · / commands · @ files · Tab expand · Enter send · Ctrl+C quit"
	}
	return style.Render(h)
}
