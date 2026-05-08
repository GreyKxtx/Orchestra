package theme

import "github.com/charmbracelet/lipgloss"

// OrchestraTheme is the default dark/purple/yellow theme.
type OrchestraTheme struct{}

func (t *OrchestraTheme) Background() lipgloss.Color        { return "#0d0d0d" }
func (t *OrchestraTheme) BackgroundSecondary() lipgloss.Color { return "#3a3a3a" }
func (t *OrchestraTheme) Primary() lipgloss.Color           { return "#9d7cd8" }
func (t *OrchestraTheme) Secondary() lipgloss.Color         { return "#e0af68" }
func (t *OrchestraTheme) Text() lipgloss.Color              { return "#e0e0e0" }
func (t *OrchestraTheme) TextMuted() lipgloss.Color         { return "#907090" }
func (t *OrchestraTheme) Error() lipgloss.Color             { return "#f7768e" }
func (t *OrchestraTheme) Success() lipgloss.Color           { return "#9ece6a" }
func (t *OrchestraTheme) Warning() lipgloss.Color           { return "#e0af68" }
func (t *OrchestraTheme) BorderNormal() lipgloss.Color      { return "#3b4261" }
func (t *OrchestraTheme) BorderFocused() lipgloss.Color     { return "#9d7cd8" }
