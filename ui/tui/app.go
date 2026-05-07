// Package tui implements the Orchestra terminal UI client.
// Phase 2 connects to orchestra core via JSON-RPC stdio.
package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/ui/tui/rpcclient"
	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/view"
)

// Config carries one-time settings into the App.
type Config struct {
	Binary        string // path to orchestra binary for spawning core subprocess (empty → echo mode)
	WorkspaceRoot string // project root passed to core
	Model         string
	Mode          string
	CWD           string
}

// App is the root Bubble Tea Model.
type App struct {
	cfg     Config
	session *state.Session
	header  view.Header
	chat    view.Chat
	input   view.Input
	footer  view.Footer

	width       int
	height      int
	initialized bool

	rpc       *rpcclient.Client
	rpcCancel context.CancelFunc
}

// rpcEventMsg wraps an rpcclient.Event for the Bubble Tea event loop.
type rpcEventMsg rpcclient.Event

// NewApp constructs an App with the given config. If cfg.Binary is non-empty,
// spawns the core subprocess and runs the initialize handshake; on error,
// returns it.
func NewApp(cfg Config) (*App, error) {
	a := &App{
		cfg:     cfg,
		header:  view.Header{Model: cfg.Model, Mode: cfg.Mode, CWD: cfg.CWD},
		footer:  view.Footer{},
		session: state.NewSession(),
	}

	if cfg.Binary != "" {
		ctx, cancel := context.WithCancel(context.Background())
		client, err := rpcclient.Spawn(ctx, rpcclient.Config{
			Binary:        cfg.Binary,
			WorkspaceRoot: cfg.WorkspaceRoot,
		})
		if err != nil {
			cancel()
			return nil, err
		}
		a.rpc = client
		a.rpcCancel = cancel
	}

	return a, nil
}

// Init satisfies tea.Model.
func (a *App) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, a.listenForEvents())
}

// listenForEvents returns a Cmd that reads one event from the rpc channel.
func (a *App) listenForEvents() tea.Cmd {
	if a.rpc == nil {
		return nil
	}
	ch := a.rpc.Events()
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return rpcEventMsg{Kind: rpcclient.EventConnectionClosed}
		}
		return rpcEventMsg(ev)
	}
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
			a.session.StartAssistant()
			a.chat.SetMessages(a.session.Messages)
			a.input.Reset()
			if a.rpc != nil {
				go func(query string) {
					_ = a.rpc.AgentRun(context.Background(), query)
				}(text)
				return a, nil
			}
			// Echo fallback (tests).
			a.session.AppendAssistantDelta("echo: " + text)
			a.session.FinishAssistant()
			a.chat.SetMessages(a.session.Messages)
			return a, nil
		}

	case rpcEventMsg:
		a.handleRPCEvent(rpcclient.Event(m))
		return a, a.listenForEvents()
	}

	// Forward to textarea.
	innerTA := a.input.Inner()
	updatedTA, cmd := innerTA.Update(msg)
	*innerTA = updatedTA
	return a, cmd
}

func (a *App) handleRPCEvent(ev rpcclient.Event) {
	switch ev.Kind {
	case rpcclient.EventMessageDelta:
		a.session.AppendAssistantDelta(ev.Content)
	case rpcclient.EventToolCallStart:
		a.session.AppendToolBlock(state.ToolBlock{
			ID:     ev.ToolCallID,
			Name:   ev.ToolCallName,
			Status: state.ToolBlockRunning,
		})
	case rpcclient.EventToolCallCompleted:
		status := state.ToolBlockCompleted
		if strings.HasPrefix(ev.Content, "error: ") {
			status = state.ToolBlockFailed
		}
		a.session.UpdateToolBlock(ev.ToolCallID, status, ev.Content)
	case rpcclient.EventStepDone:
		// Cosmetic for Phase 2.
	case rpcclient.EventDone, rpcclient.EventAgentRunCompleted:
		a.session.FinishAssistant()
	case rpcclient.EventError, rpcclient.EventConnectionError:
		a.session.AppendMessage(state.Message{
			Role: state.RoleSystem,
			Text: "[error] " + ev.Err,
		})
	case rpcclient.EventPendingOps:
		if ev.PendingOps != nil {
			count := len(ev.PendingOps.Ops)
			a.session.AppendMessage(state.Message{
				Role: state.RoleSystem,
				Text: fmt.Sprintf("[%d pending ops — apply with /apply (Phase 3)]", count),
			})
		}
	}
	a.chat.SetMessages(a.session.Messages)
}

// View renders the full screen layout.
func (a *App) View() string {
	if a.width == 0 || a.height == 0 {
		return ""
	}
	return a.header.Render() + "\n" + a.chat.Render() + "\n" + a.input.Render() + "\n" + a.footer.Render()
}

// layout recomputes child sizes based on current width/height.
func (a *App) layout() {
	a.header.SetSize(a.width)
	a.footer.SetSize(a.width)

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
	app, err := NewApp(cfg)
	if err != nil {
		return err
	}
	defer func() {
		if app.rpc != nil {
			_ = app.rpc.Close()
		}
		if app.rpcCancel != nil {
			app.rpcCancel()
		}
	}()
	p := tea.NewProgram(app, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
