package theme

import "github.com/charmbracelet/lipgloss"

// OrchestraTheme is the default dark/purple/yellow theme.
type OrchestraTheme struct{}

func (t *OrchestraTheme) Background() lipgloss.Color          { return "#000000" }
func (t *OrchestraTheme) BackgroundSecondary() lipgloss.Color { return "#1a1a1a" }
func (t *OrchestraTheme) Primary() lipgloss.Color             { return "#6b42a0" }
func (t *OrchestraTheme) Secondary() lipgloss.Color           { return "#a07830" }
func (t *OrchestraTheme) Text() lipgloss.Color                { return "#e0e0e0" }
func (t *OrchestraTheme) TextMuted() lipgloss.Color           { return "#888888" }
func (t *OrchestraTheme) Error() lipgloss.Color               { return "#f7768e" }
func (t *OrchestraTheme) Success() lipgloss.Color             { return "#9ece6a" }
func (t *OrchestraTheme) Warning() lipgloss.Color             { return "#a07830" }
func (t *OrchestraTheme) BorderNormal() lipgloss.Color        { return "#2a2d45" }
func (t *OrchestraTheme) BorderFocused() lipgloss.Color       { return "#6b42a0" }

// ModeAccent — for the purple/yellow Orchestra theme, mode badges use the
// theme's existing palette rather than fresh hues to keep visual coherence.
func (t *OrchestraTheme) ModeAccent(mode string) lipgloss.Color {
	switch mode {
	case "explore", "ask":
		return "#3a5fa0" // dark blue — read-only / exploration
	case "plan", "architecture":
		return "#9a7028" // dark gold — planning
	case "debug":
		return "#a04050"
	case "agent":
		return "#5b3d8a"
	case "orchestra", "worker":
		return "#2f4f8f"
	default:
		return "#6b42a0" // dark purple — build (matches Primary)
	}
}
