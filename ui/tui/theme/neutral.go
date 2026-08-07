package theme

import "github.com/charmbracelet/lipgloss"

// NeutralTheme is the default Orchestra palette: pure black background with
// shades of grey and white. The only saturated colors are reserved for
// semantic status (Success / Warning / Error). "Primary" and "Secondary"
// are intentionally desaturated so accents read as part of one quiet system
// rather than competing rainbow elements.
type NeutralTheme struct{}

func (t *NeutralTheme) Background() lipgloss.Color          { return "#000000" }
func (t *NeutralTheme) BackgroundSecondary() lipgloss.Color { return "#2a2a2a" }
func (t *NeutralTheme) Primary() lipgloss.Color             { return "#c0c0c0" } // light grey: highlight / selection
func (t *NeutralTheme) Secondary() lipgloss.Color           { return "#888888" } // medium grey: secondary accent
func (t *NeutralTheme) Text() lipgloss.Color                { return "#ffffff" }
func (t *NeutralTheme) TextMuted() lipgloss.Color           { return "#888888" }
func (t *NeutralTheme) Error() lipgloss.Color               { return "#f7768e" }
func (t *NeutralTheme) Success() lipgloss.Color             { return "#9ece6a" }
func (t *NeutralTheme) Warning() lipgloss.Color             { return "#e0af68" }
func (t *NeutralTheme) BorderNormal() lipgloss.Color        { return "#444444" }
func (t *NeutralTheme) BorderFocused() lipgloss.Color       { return "#c0c0c0" }

// ModeAccent — mode badges keep the only saturated hue in the neutral theme,
// so the user can spot the active agent mode at a glance. Mode names mirror
// internal/config's built-in modes (build / plan / explore / ...).
func (t *NeutralTheme) ModeAccent(mode string) lipgloss.Color {
	switch mode {
	case "explore", "ask":
		return "#7dcfff" // cyan — read-only / exploration
	case "plan", "architecture":
		return "#e0af68" // yellow — planning / design
	case "debug":
		return "#f7768e" // rose — investigation
	case "agent":
		return "#bb9af7" // violet — auto-route
	case "orchestra", "worker":
		return "#7aa2f7" // blue — lead/worker
	default:
		return "#9ece6a" // green (build) — active edits
	}
}
