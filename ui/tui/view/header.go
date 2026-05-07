// Package view holds the per-region Bubble Tea views for the TUI.
package view

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// Header is the top-of-screen status line.
// Shows: Orchestra · model · mode · cwd
type Header struct {
	Model string // e.g. "qwen3.6-27b"
	Mode  string // e.g. "code"
	CWD   string // current working directory (truncated for display)

	width int
}

// SetSize updates the header's known width for layout.
func (h *Header) SetSize(width int) {
	h.width = width
}

// Render returns the styled header line.
func (h Header) Render() string {
	style := lipgloss.NewStyle().
		Background(lipgloss.Color("#3a3a3a")).
		Foreground(lipgloss.Color("#ffffff")).
		Padding(0, 1).
		Width(h.width)

	parts := fmt.Sprintf("Orchestra · %s · %s · %s", h.Model, h.Mode, h.CWD)
	return style.Render(parts)
}
