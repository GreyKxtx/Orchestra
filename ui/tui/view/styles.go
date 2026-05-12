package view

import (
	"sync"

	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/theme"
)

// Styles holds pre-built lipgloss.Style values for the most common foreground
// + background combinations rendered on the per-tick UI path. Building a
// fresh lipgloss.Style is cheap individually, but at ~10K NewStyle/sec on a
// busy session the allocations add up.
//
// Styles is invalidated automatically when the active theme changes — request
// the current set via CurrentStyles(); never cache a returned struct longer
// than one render pass.
type Styles struct {
	t           theme.Theme
	Muted       lipgloss.Style // muted fg, no bg
	Text        lipgloss.Style // primary text fg
	TextBold    lipgloss.Style
	Warning     lipgloss.Style
	Error       lipgloss.Style
	Success     lipgloss.Style
	Primary     lipgloss.Style

	// Panel-bg variants (BackgroundSecondary fill).
	PanelMuted    lipgloss.Style
	PanelText     lipgloss.Style
	PanelTextBold lipgloss.Style
}

var (
	stylesMu     sync.RWMutex
	stylesCached *Styles
)

// CurrentStyles returns the cached style sheet for the active theme,
// rebuilding it the first time it is requested or when the theme has changed.
func CurrentStyles() *Styles {
	t := theme.CurrentTheme()
	stylesMu.RLock()
	cached := stylesCached
	stylesMu.RUnlock()
	if cached != nil && cached.t == t {
		return cached
	}

	stylesMu.Lock()
	defer stylesMu.Unlock()
	if stylesCached != nil && stylesCached.t == t {
		return stylesCached
	}
	bg := t.BackgroundSecondary()
	s := &Styles{
		t:        t,
		Muted:    lipgloss.NewStyle().Foreground(t.TextMuted()),
		Text:     lipgloss.NewStyle().Foreground(t.Text()),
		TextBold: lipgloss.NewStyle().Foreground(t.Text()).Bold(true),
		Warning:  lipgloss.NewStyle().Foreground(t.Warning()),
		Error:    lipgloss.NewStyle().Foreground(t.Error()),
		Success:  lipgloss.NewStyle().Foreground(t.Success()),
		Primary:  lipgloss.NewStyle().Foreground(t.Primary()),

		PanelMuted:    lipgloss.NewStyle().Background(bg).Foreground(t.TextMuted()),
		PanelText:     lipgloss.NewStyle().Background(bg).Foreground(t.Text()),
		PanelTextBold: lipgloss.NewStyle().Background(bg).Foreground(t.Text()).Bold(true),
	}
	stylesCached = s
	return s
}

// InvalidateStyles forces the next CurrentStyles call to rebuild. Called by
// theme.SetTheme so external switches are picked up automatically.
func InvalidateStyles() {
	stylesMu.Lock()
	stylesCached = nil
	stylesMu.Unlock()
}
