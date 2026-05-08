package theme

import "github.com/charmbracelet/lipgloss"

// Theme defines the color contract for all TUI view components.
type Theme interface {
	Background() lipgloss.Color
	BackgroundSecondary() lipgloss.Color
	Primary() lipgloss.Color   // assistant border, selected items
	Secondary() lipgloss.Color // user border, accents
	Text() lipgloss.Color
	TextMuted() lipgloss.Color // hints, metadata, modal borders
	Error() lipgloss.Color
	Success() lipgloss.Color
	Warning() lipgloss.Color
	BorderNormal() lipgloss.Color
	BorderFocused() lipgloss.Color
}

var current Theme = &OrchestraTheme{}

// CurrentTheme returns the active theme. Never nil.
func CurrentTheme() Theme { return current }

// SetTheme replaces the active theme (for testing or theme switching).
func SetTheme(t Theme) { current = t }
