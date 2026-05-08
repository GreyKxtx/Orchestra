// Package tui implements the Orchestra terminal UI client.
// Phase 2 connects to orchestra core via JSON-RPC stdio.
package tui

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

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

	slashPalette  *view.SlashPalette
	paletteActive bool

	mentionPalette *view.MentionPalette
	mentionActive  bool
	workspaceFiles []string // lazily populated on first @-mention

	history *state.InputHistory
}

// rpcEventMsg wraps an rpcclient.Event for the Bubble Tea event loop.
type rpcEventMsg rpcclient.Event

// applyResultMsg is returned by the ops-apply Cmd to keep session writes on the Update goroutine.
type applyResultMsg struct {
	err   error
	count int
}

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
	a.slashPalette = view.NewSlashPalette(0)   // width set in layout()
	a.mentionPalette = view.NewMentionPalette(0) // width set in layout()
	a.history = state.NewInputHistory(100)

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
		case "up":
			if a.paletteActive {
				a.slashPalette.CursorUp()
				return a, nil
			}
			if a.mentionActive {
				a.mentionPalette.CursorUp()
				return a, nil
			}
			// History navigation: only when input has no newline (single-line mode).
			if !strings.Contains(a.input.Value(), "\n") {
				text := a.history.Up(a.input.Value())
				a.input.SetValue(text)
				return a, nil
			}
		case "down":
			if a.paletteActive {
				a.slashPalette.CursorDown()
				return a, nil
			}
			if a.mentionActive {
				a.mentionPalette.CursorDown()
				return a, nil
			}
			if !strings.Contains(a.input.Value(), "\n") && a.history.IsNavigating() {
				text := a.history.Down()
				a.input.SetValue(text)
				return a, nil
			}
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
			if a.mentionActive {
				a.mentionActive = false
				a.layout()
				a.updateFooter()
				return a, nil
			}
			if a.paletteActive {
				a.paletteActive = false
				a.input.Reset()
				a.layout()
				a.updateFooter()
				return a, nil
			}
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
			if a.mentionActive {
				if sel := a.mentionPalette.Selected(); sel != "" {
					a.input.SetValue(replaceLastMention(a.input.Value(), sel))
					a.mentionActive = false
					a.syncPalette()
					a.layout()
					a.updateFooter()
				}
				return a, nil
			}
			// existing: toggle last tool block
			a.session.ToggleLastToolBlock()
			a.chat.SetMessages(a.session.Messages)
			return a, nil

		case "a":
			if a.pendingOps != nil && a.rpc != nil {
				rawOps := a.pendingOps.Ops
				count := len(a.pendingOps.Ops)
				a.pendingOps = nil
				if a.diffShown {
					a.session.RemoveDiff()
					a.diffShown = false
				}
				a.chat.SetMessages(a.session.Messages)
				a.layout()
				a.updateFooter()
				rpc := a.rpc
				return a, func() tea.Msg {
					return applyResultMsg{err: rpc.ApplyOps(context.Background(), rawOps), count: count}
				}
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
			if a.mentionActive {
				if sel := a.mentionPalette.Selected(); sel != "" {
					a.input.SetValue(replaceLastMention(a.input.Value(), sel))
				}
				a.mentionActive = false
				a.syncPalette()
				a.layout()
				a.updateFooter()
				return a, nil
			}
			if a.paletteActive {
				selectedCmd := a.slashPalette.Selected()
				a.paletteActive = false
				a.input.Reset()
				a.layout()
				cmd := a.executePaletteCmd(selectedCmd)
				a.updateFooter()
				return a, cmd
			}
			if a.agentBusy {
				return a, nil
			}
			text := strings.TrimSpace(a.input.Value())
			if text == "" {
				return a, nil
			}
			a.session.AppendMessage(state.Message{Role: state.RoleUser, Text: text})
			a.session.StartAssistant()
			a.chat.SetMessages(a.session.Messages)
			a.history.Push(text)
			a.history.Reset()
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

	case applyResultMsg:
		if m.err != nil {
			a.session.AppendMessage(state.Message{Role: state.RoleSystem, Text: "[apply failed] " + m.err.Error()})
		} else {
			a.session.AppendMessage(state.Message{Role: state.RoleSystem, Text: fmt.Sprintf("[applied %d ops]", m.count)})
		}
		a.chat.SetMessages(a.session.Messages)
		return a, nil
	}

	// Forward all messages to textarea.
	innerTA := a.input.Inner()
	updatedTA, taCmd := innerTA.Update(msg)
	*innerTA = updatedTA
	// Sync palette state after key events that may change input value.
	if _, isKey := msg.(tea.KeyMsg); isKey {
		a.syncPalette()
		a.updateFooter()
		a.layout()
	}
	return a, taCmd
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
			a.layout()
		}
	case rpcclient.EventPermissionRequest:
		if ev.PermReq != nil {
			a.permModal = view.NewModal(ev.PermReq.Tool, ev.PermReq.Description)
			a.layout()
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
	if a.paletteActive && len(a.slashPalette.Items) > 0 {
		parts = append(parts, a.slashPalette.Render())
	}
	if a.mentionActive && len(a.mentionPalette.Items) > 0 {
		parts = append(parts, a.mentionPalette.Render())
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
	paletteRows := 0
	if a.paletteActive && len(a.slashPalette.Items) > 0 {
		n := len(a.slashPalette.Items)
		if n > 6 {
			n = 6
		}
		paletteRows = n + 2 // +2 for rounded border lines
	} else if a.mentionActive && len(a.mentionPalette.Items) > 0 {
		n := len(a.mentionPalette.Items)
		if n > 6 {
			n = 6
		}
		paletteRows = n + 2
	}
	inputRows := 4
	modalRows := 0
	if a.permModal != nil {
		inputRows = 0 // modal replaces input
		modalRows = 5
		a.permModal.SetSize(a.width)
	}
	chatHeight := a.height - 1 - 1 - inputRows - actionBarRows - modalRows - paletteRows
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
	a.slashPalette.SetSize(a.width)
	a.mentionPalette.SetSize(a.width)
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

// syncPalette checks the current input value and opens/closes the slash palette.
func (a *App) syncPalette() {
	val := a.input.Value()
	if strings.HasPrefix(val, "/") {
		a.slashPalette.Filter(val[1:])
		a.paletteActive = len(a.slashPalette.Items) > 0
		a.mentionActive = false
		return
	}
	a.paletteActive = false
	a.syncMention()
}

// syncMention checks if the input ends with an @-mention and updates the mention palette.
func (a *App) syncMention() {
	if !strings.Contains(a.input.Value(), "@") {
		a.mentionActive = false
		return
	}
	// Lazy-load workspace files.
	if a.workspaceFiles == nil && a.cfg.WorkspaceRoot != "" {
		a.workspaceFiles = listWorkspaceFiles(a.cfg.WorkspaceRoot, 4)
	}

	q := mentionQuery(a.input.Value())
	lastAt := strings.LastIndex(a.input.Value(), "@")
	if lastAt < 0 {
		a.mentionActive = false
		return
	}

	var items []string
	if q == "" {
		// Show first 10 files when just "@" is typed.
		items = a.workspaceFiles
		if len(items) > 10 {
			items = items[:10]
		}
	} else {
		matches := fuzzy.Find(q, a.workspaceFiles)
		items = make([]string, 0, len(matches))
		for _, m := range matches {
			items = append(items, m.Str)
			if len(items) >= 10 {
				break
			}
		}
	}
	a.mentionPalette.SetItems(items)
	a.mentionActive = len(items) > 0
}

// listWorkspaceFiles walks root up to maxDepth levels, skipping hidden dirs,
// and returns relative POSIX paths. Result is capped at 500 entries.
func listWorkspaceFiles(root string, maxDepth int) []string {
	if root == "" {
		return nil
	}
	var files []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			rel, _ := filepath.Rel(root, path)
			depth := len(strings.Split(rel, string(filepath.Separator)))
			if depth > maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		files = append(files, filepath.ToSlash(rel))
		if len(files) >= 500 {
			return filepath.SkipDir
		}
		return nil
	})
	return files
}

// mentionQuery returns the text after @ in the last @word of the input,
// or "" if the last word is not an @-mention or has no text after @.
func mentionQuery(text string) string {
	lastAt := strings.LastIndex(text, "@")
	if lastAt < 0 {
		return ""
	}
	word := text[lastAt+1:]
	// If there's a space after @, it's not a single @word at the end.
	if strings.IndexByte(word, ' ') >= 0 {
		return ""
	}
	return word
}

// replaceLastMention replaces the last @word in text with replacement.
func replaceLastMention(text, replacement string) string {
	lastAt := strings.LastIndex(text, "@")
	if lastAt < 0 {
		return text
	}
	rest := text[lastAt+1:]
	spaceIdx := strings.IndexByte(rest, ' ')
	if spaceIdx < 0 {
		return text[:lastAt] + replacement
	}
	return text[:lastAt] + replacement + rest[spaceIdx:]
}

const helpText = `Orchestra TUI — key bindings:
  Enter         send message
  Shift+Enter   newline in input
  Tab           expand/collapse last tool block
  ↑ / ↓         input history (single-line mode)
  /             slash command palette
  @             file mention (fuzzy)
  a             apply pending ops
  d             toggle diff
  x             discard pending ops
  y / n / Esc   allow / deny exec.run permission
  Ctrl+C        quit`

// executePaletteCmd carries out the chosen slash command and returns a tea.Cmd
// if the action requires async work, or nil.
func (a *App) executePaletteCmd(cmd string) tea.Cmd {
	switch cmd {
	case "/help":
		a.session.AppendMessage(state.Message{Role: state.RoleSystem, Text: helpText})
		a.chat.SetMessages(a.session.Messages)
	case "/clear":
		a.session.Clear()
		a.chat.SetMessages(a.session.Messages)
	case "/model":
		model := a.cfg.Model
		if model == "" {
			model = "(not configured)"
		}
		a.session.AppendMessage(state.Message{Role: state.RoleSystem, Text: "Model: " + model})
		a.chat.SetMessages(a.session.Messages)
	case "/mode":
		mode := a.cfg.Mode
		if mode == "" {
			mode = "(not configured)"
		}
		a.session.AppendMessage(state.Message{Role: state.RoleSystem, Text: "Mode: " + mode})
		a.chat.SetMessages(a.session.Messages)
	case "/diff":
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
		}
	case "/apply":
		if a.pendingOps != nil && a.rpc != nil {
			rawOps := a.pendingOps.Ops
			count := len(a.pendingOps.Ops)
			a.pendingOps = nil
			if a.diffShown {
				a.session.RemoveDiff()
				a.diffShown = false
			}
			a.chat.SetMessages(a.session.Messages)
			rpc := a.rpc
			return func() tea.Msg {
				return applyResultMsg{err: rpc.ApplyOps(context.Background(), rawOps), count: count}
			}
		}
	case "/discard":
		if a.pendingOps != nil {
			a.pendingOps = nil
			if a.diffShown {
				a.session.RemoveDiff()
				a.diffShown = false
			}
			a.chat.SetMessages(a.session.Messages)
		}
	case "/quit":
		return tea.Quit
	}
	return nil
}

func (a *App) updateFooter() {
	switch {
	case a.paletteActive:
		a.footer.SetHints("↑↓ select · Enter execute · Esc cancel")
	case a.mentionActive:
		a.footer.SetHints("↑↓ select · Enter/Tab insert · Esc cancel")
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
