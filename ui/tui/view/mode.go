package view

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/theme"
)

// ModeColor returns the accent color used for the agent-mode badge. The
// concrete hue is owned by the active theme so a theme switch reskins every
// mode-colored span (input border, ┃ accent, footer ▣) in one place.
func ModeColor(mode string) lipgloss.Color {
	return theme.CurrentTheme().ModeAccent(mode)
}
