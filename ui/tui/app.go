// Package tui implements the Orchestra terminal UI client.
// In Phase 1 it provides an echo-only skeleton; Phase 2 connects it
// to orchestra core via JSON-RPC stdio.
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/view"
)

// Config carries one-time settings into the App.
type Config struct {
	Model string // for the header
	Mode  string // for the header
	CWD   string // for the header
}

// App is the root Bubble Tea Model.
type App struct {
	cfg     Config
	session state.Session
	header  view.Header
	chat    view.Chat
	input   view.Input
	footer  view.Footer

	width       int
	height      int
	initialized bool
}

// NewApp constructs an App with the given config.
func NewApp(cfg Config) *App {
	return &App{
		cfg:    cfg,
		header: view.Header{Model: cfg.Model, Mode: cfg.Mode, CWD: cfg.CWD},
		footer: view.Footer{},
	}
}

// Init satisfies tea.Model.
func (a *App) Init() tea.Cmd {
	return nil
}

// Update is implemented in Task 3.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return a, nil
}

// View renders the full screen layout.
func (a *App) View() string {
	if a.width == 0 || a.height == 0 {
		return ""
	}
	return a.header.Render() + "\n" + a.chat.Render() + "\n" + a.input.Render() + "\n" + a.footer.Render()
}

// Run starts the tea program. Blocks until quit.
func Run(cfg Config) error {
	app := NewApp(cfg)
	p := tea.NewProgram(app, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
