package view

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// Modal displays a blocking yes/no dialog for exec.run consent.
type Modal struct {
	Tool        string
	Description string
	width       int
}

// NewModal creates a permission modal for the given tool request.
func NewModal(tool, description string) *Modal {
	return &Modal{Tool: tool, Description: description}
}

// SetSize updates the modal's known width.
func (m *Modal) SetSize(width int) { m.width = width }

// Render returns a styled permission prompt string.
func (m *Modal) Render() string {
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#f7768e")).
		Padding(0, 2).
		Width(m.width - 4)

	title := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#f7768e")).
		Bold(true).
		Render("⚠ exec.run permission request")

	desc := m.Description
	if len(desc) > 200 {
		desc = desc[:200] + "..."
	}
	body := fmt.Sprintf("%s\n\nCommand: %s\n\n[y] Allow   [n] Deny", title, desc)
	return border.Render(body)
}
