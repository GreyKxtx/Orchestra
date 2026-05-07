package view

import "github.com/charmbracelet/lipgloss"

// Footer is the bottom-of-screen hints line.
type Footer struct {
	width int
}

// SetSize updates the footer's known width.
func (f *Footer) SetSize(width int) {
	f.width = width
}

// Render returns the styled footer hints.
func (f Footer) Render() string {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		Width(f.width).
		Padding(0, 1)
	return style.Render("↑↓ history · Enter send · Shift+Enter newline · Ctrl+C quit")
}
