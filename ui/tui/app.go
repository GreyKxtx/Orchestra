// Package tui implements the Orchestra terminal UI client.
// Phase 2 connects to orchestra core via JSON-RPC stdio.
package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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

	pendingOps *rpcclient.PendingOpsPayload // non-nil while ops await confirmation
	diffShown  bool                          // true while diff messages are in session
	agentBusy  bool                          // true while agent.run in flight

	permModal *view.Modal // non-nil while an exec.run permission request is pending
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
		case "y":
			if a.permModal != nil {
				a.permModal = nil
				a.updateFooter()
				if a.rpc != nil {
					a.rpc.RespondPermission(true)
				}
				return a, nil
			}
		case "n":
			if a.permModal != nil {
				a.permModal = nil
				a.updateFooter()
				if a.rpc != nil {
					a.rpc.RespondPermission(false)
				}
				return a, nil
			}
		case "ctrl+c":
			return a, tea.Quit
		case "esc":
			if a.permModal != nil {
				a.permModal = nil
				a.updateFooter()
				if a.rpc != nil {
					a.rpc.RespondPermission(false)
				}
				return a, nil
			}
			a.input.Reset()
			return a, nil
		case "tab":
			a.session.ToggleLastToolBlock()
			a.chat.SetMessages(a.session.Messages)
			return a, nil

		case "a":
			if a.pendingOps != nil && a.rpc != nil {
				ops := a.pendingOps.Ops
				pending := a.pendingOps
				a.pendingOps = nil
				if a.diffShown {
					a.session.RemoveDiff()
					a.diffShown = false
				}
				a.chat.SetMessages(a.session.Messages)
				a.layout()
				a.updateFooter()
				go func() {
					if err := a.rpc.ApplyOps(context.Background(), ops); err != nil {
						a.session.AppendMessage(state.Message{
							Role: state.RoleSystem,
							Text: "[apply failed] " + err.Error(),
						})
					} else {
						a.session.AppendMessage(state.Message{
							Role: state.RoleSystem,
							Text: fmt.Sprintf("[applied %d ops]", len(pending.Ops)),
						})
					}
					a.chat.SetMessages(a.session.Messages)
				}()
				return a, nil
			}

		case "d":
			if a.pendingOps != nil {
				if a.diffShown {
					a.session.RemoveDiff()
					a.diffShown = false
				} else {
					content := a.buildDiffContent()
					a.session.AddDiff(content)
					a.diffShown = true
				}
				a.chat.SetMessages(a.session.Messages)
				a.updateFooter()
				return a, nil
			}

		case "x":
			if a.pendingOps != nil {
				a.pendingOps = nil
				if a.diffShown {
					a.session.RemoveDiff()
					a.diffShown = false
				}
				a.chat.SetMessages(a.session.Messages)
				a.layout()
				a.updateFooter()
				return a, nil
			}

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
				a.agentBusy = true
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
		a.agentBusy = false
	case rpcclient.EventError, rpcclient.EventConnectionError:
		a.session.AppendMessage(state.Message{
			Role: state.RoleSystem,
			Text: "[error] " + ev.Err,
		})
	case rpcclient.EventPendingOps:
		if ev.PendingOps != nil && !ev.PendingOps.Applied {
			a.pendingOps = ev.PendingOps
		}
	case rpcclient.EventPermissionRequest:
		if ev.PermReq != nil {
			a.permModal = view.NewModal(ev.PermReq.Tool, ev.PermReq.Description)
			a.permModal.SetSize(a.width)
			a.updateFooter()
		}
	}
	a.chat.SetMessages(a.session.Messages)
	a.updateFooter()
}

// View renders the full screen layout.
func (a *App) View() string {
	if a.width == 0 || a.height == 0 {
		return ""
	}
	parts := []string{a.header.Render(), a.chat.Render()}
	if a.pendingOps != nil {
		parts = append(parts, a.renderActionBar())
	}
	if a.permModal != nil {
		parts = append(parts, a.permModal.Render())
	} else {
		parts = append(parts, a.input.Render())
	}
	parts = append(parts, a.footer.Render())
	return strings.Join(parts, "\n")
}

func (a *App) renderActionBar() string {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#e0af68")).
		Width(a.width).
		Padding(0, 1)
	count := len(a.pendingOps.Ops)
	return style.Render(fmt.Sprintf("⏵ %d pending ops · [a]pply · [d]iff · [x]discard", count))
}

// layout recomputes child sizes based on current width/height.
func (a *App) layout() {
	a.header.SetSize(a.width)
	a.footer.SetSize(a.width)

	actionBarRows := 0
	if a.pendingOps != nil {
		actionBarRows = 1
	}
	modalRows := 0
	if a.permModal != nil {
		modalRows = 5
		a.permModal.SetSize(a.width)
	}
	chatHeight := a.height - 1 - 1 - 4 - actionBarRows - modalRows
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

func (a *App) buildDiffContent() string {
	if a.pendingOps == nil || len(a.pendingOps.Diff) == 0 {
		return "(no diff available)"
	}
	diffs := make([]view.FileDiffView, len(a.pendingOps.Diff))
	for i, fd := range a.pendingOps.Diff {
		diffs[i] = view.FileDiffView{Path: fd.Path, Before: fd.Before, After: fd.After}
	}
	return view.RenderAllDiffs(diffs, a.width)
}

func (a *App) updateFooter() {
	switch {
	case a.permModal != nil:
		a.footer.SetHints("[y]es allow · [n]o deny · Esc deny")
	case a.pendingOps != nil:
		a.footer.SetHints("[a]pply · [d]iff · [x]discard · Ctrl+C quit")
	default:
		a.footer.SetHints("")
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
