package view

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/orchestra/orchestra/ui/tui/theme"
)

// Header is the top-of-screen status line.
type Header struct {
	Model string
	Mode  string
	CWD   string
	width int
}

func (h *Header) SetSize(width int) { h.width = width }

func (h Header) Render() string {
	t := theme.CurrentTheme()
	style := lipgloss.NewStyle().
		Background(t.BackgroundSecondary()).
		Foreground(t.TextMuted()).
		Padding(0, 1).
		Width(h.width)

	accent := lipgloss.NewStyle().
		Background(t.BackgroundSecondary()).
		Foreground(t.Primary()).
		Bold(true)

	name := accent.Render("♪ Orchestra")
	parts := name + "  ·  " + h.Model + "  ·  " + h.CWD
	return style.Render(parts)
}
