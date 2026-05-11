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

	// ModeAccent returns the accent color for one of the agent modes
	// ("build", "ask", "plan"). Unknown mode → build color.
	ModeAccent(mode string) lipgloss.Color
}

// registry maps theme names to their concrete implementations. New themes
// register themselves via Register(); ByName looks up by lowercase name.
var registry = map[string]Theme{
	"neutral":   &NeutralTheme{},
	"orchestra": &OrchestraTheme{},
}

// DefaultTheme is the name of the theme used when nothing is configured or
// the configured theme is unknown.
const DefaultTheme = "orchestra"

var current Theme = registry[DefaultTheme]

// CurrentTheme returns the active theme. Never nil.
func CurrentTheme() Theme { return current }

// SetTheme replaces the active theme (for testing or theme switching).
func SetTheme(t Theme) {
	if t != nil {
		current = t
	}
}

// ByName returns the registered theme matching name, or the default theme
// when name is empty / unknown.
func ByName(name string) Theme {
	if t, ok := registry[name]; ok {
		return t
	}
	return registry[DefaultTheme]
}

// Register adds (or replaces) a theme by name. Useful for plugins or tests
// that want to install custom palettes.
func Register(name string, t Theme) {
	if t != nil && name != "" {
		registry[name] = t
	}
}

// AvailableThemes returns the list of registered theme names. Order is not
// guaranteed; callers that need stable order should sort.
func AvailableThemes() []string {
	out := make([]string, 0, len(registry))
	for name := range registry {
		out = append(out, name)
	}
	return out
}
