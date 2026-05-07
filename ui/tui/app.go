// Package tui implements the Orchestra terminal UI client.
// In Phase 1 it provides an echo-only skeleton; Phase 2 connects it
// to orchestra core via JSON-RPC stdio.
package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
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
	return textarea.Blink
}

// Update routes incoming messages.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = m.Width
		a.height = m.Height
		a.layout()
		return a, nil

	case tea.KeyMsg:
		switch m.String() {
		case "ctrl+c":
			return a, tea.Quit
		case "esc":
			a.input.Reset()
			return a, nil
		case "enter":
			text := strings.TrimSpace(a.input.Value())
			if text == "" {
				return a, nil
			}
			a.session.AppendMessage(state.Message{Role: state.RoleUser, Text: text})
			// Phase 1: synthesize an echo response so we can verify the round-trip.
			a.session.AppendMessage(state.Message{Role: state.RoleAssistant, Text: "echo: " + text})
			a.chat.SetMessages(a.session.Messages)
			a.input.Reset()
			return a, nil
		}
	}

	// Forward all other messages (most KeyMsg for typing, blink, etc.) to the textarea.
	innerTA := a.input.Inner()
	updatedTA, cmd := innerTA.Update(msg)
	*innerTA = updatedTA
	return a, cmd
}

// View renders the full screen layout.
func (a *App) View() string {
	if a.width == 0 || a.height == 0 {
		return ""
	}
	return a.header.Render() + "\n" + a.chat.Render() + "\n" + a.input.Render() + "\n" + a.footer.Render()
}

// layout recomputes child sizes based on current width/height.
// Lazily initializes chat and input on first call.
func (a *App) layout() {
	a.header.SetSize(a.width)
	a.footer.SetSize(a.width)

	// Reserve: 1 line header, 1 line footer, 4 lines for input area (textarea + spacing).
	chatHeight := a.height - 1 - 1 - 4
	if chatHeight < 1 {
		chatHeight = 1
	}

	if !a.initialized {
		a.chat = view.NewChat(a.width, chatHeight)
		a.input = view.NewInput(a.width)
		a.initialized = true
	} else {
		a.chat.SetSize(a.width, chatHeight)
		a.input.SetSize(a.width)
	}
}

// Run starts the tea program. Blocks until quit.
func Run(cfg Config) error {
	app := NewApp(cfg)
	p := tea.NewProgram(app, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
