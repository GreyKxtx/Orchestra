// Package tui implements the Orchestra terminal UI client.
// Phase 6: visual redesign + onboarding + Ctrl+K palette modal.
package tui

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/lmstudio"
	"github.com/orchestra/orchestra/ui/tui/rpcclient"
	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/view"
)

// Config carries one-time settings into the App.
type Config struct {
	Binary          string // path to orchestra binary for spawning core subprocess (empty → echo mode)
	WorkspaceRoot   string // project root passed to core
	ProjectID       string // passed to initialize handshake
	Model           string
	Mode            string
	CWD             string
	NeedsOnboarding bool   // true when no model is configured
	ConfigPath      string // path to .orchestra.yml for saving onboarding result
}

// App is the root Bubble Tea Model.
type App struct {
	cfg     Config
	session *state.Session
	header  view.Header
	chat    view.Chat
	input   view.Input
	statusBar view.StatusBar // replaces footer

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

	// Phase 6 additions
	commandModal   *view.PaletteModal   // Ctrl+K modal
	onboarding     *view.OnboardingView // 3-step wizard
	showOnboarding bool
	cursorBlink    bool // toggles every 500ms while agentBusy
}

// rpcEventMsg wraps an rpcclient.Event for the Bubble Tea event loop.
type rpcEventMsg rpcclient.Event

// applyResultMsg is returned by the ops-apply Cmd to keep session writes on the Update goroutine.
type applyResultMsg struct {
	err   error
	count int
}

// tickMsg drives the spinner and streaming cursor animation.
type tickMsg time.Time

// modelsLoadedMsg carries model list fetched from LM Studio.
type modelsLoadedMsg struct {
	models []lmstudio.RemoteModel
	err    error
}

// onboardingDoneMsg is sent when user completes onboarding and config is saved.
type onboardingDoneMsg struct {
	configPath string
}

// rpcSpawnedMsg is sent when RPC client is ready after onboarding.
type rpcSpawnedMsg struct {
	client *rpcclient.Client
	cancel context.CancelFunc
	err    error
}

// NewApp constructs an App with the given config. If cfg.Binary is non-empty,
// spawns the core subprocess and runs the initialize handshake; on error,
// returns it.
func NewApp(cfg Config) (*App, error) {
	a := &App{
		cfg:     cfg,
		header:  view.Header{Model: cfg.Model, Mode: cfg.Mode, CWD: cfg.CWD},
		session: state.NewSession(),
	}
	a.statusBar.SetModel(cfg.Model)
	a.slashPalette = view.NewSlashPalette(0)   // width set in layout()
	a.mentionPalette = view.NewMentionPalette(0) // width set in layout()
	a.history = state.NewInputHistory(100)

	if cfg.NeedsOnboarding {
		a.showOnboarding = true
		a.onboarding = view.NewOnboardingView(80, 24)
		return a, nil
	}

	if cfg.Binary != "" {
		ctx, cancel := context.WithCancel(context.Background())
		client, err := rpcclient.Spawn(ctx, rpcclient.Config{
			Binary:        cfg.Binary,
			WorkspaceRoot: cfg.WorkspaceRoot,
			ProjectID:     cfg.ProjectID,
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
	return tea.Batch(textarea.Blink, a.listenForEvents(), tickCmd())
}

// tickCmd schedules the next tick at 500ms for spinner/cursor animation.
func tickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// fetchModelsCmd fetches models from the given LM Studio endpoint asynchronously.
func fetchModelsCmd(endpoint string) tea.Cmd {
	return func() tea.Msg {
		client := lmstudio.NewClient(endpoint)
		models, err := client.ListModels()
		return modelsLoadedMsg{models: models, err: err}
	}
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

	case tickMsg:
		a.cursorBlink = !a.cursorBlink
		a.statusBar.AdvanceSpin()
		if a.agentBusy {
			a.chat.SetStreamCursor(a.cursorBlink)
			a.chat.SetMessages(a.session.Messages)
		}
		return a, tickCmd()

	case modelsLoadedMsg:
		if a.onboarding != nil {
			a.onboarding.LoadingModels = false
			if m.err != nil {
				a.onboarding.ModelError = "LM Studio недоступен: " + m.err.Error()
			} else {
				a.onboarding.Models = m.models
				a.onboarding.ModelError = ""
			}
		}
		return a, nil

	case onboardingDoneMsg:
		a.showOnboarding = false
		cfg, err := config.Load(m.configPath)
		if err != nil {
			a.session.AppendMessage(state.Message{Role: state.RoleSystem, Text: "[error] failed to load config: " + err.Error()})
			a.chat.SetMessages(a.session.Messages)
			return a, nil
		}
		a.cfg.Model = cfg.LLM.Model
		a.header.Model = cfg.LLM.Model
		a.statusBar.SetModel(cfg.LLM.Model)
		a.chat.SetWelcomeInfo(a.buildWelcomeInfo())
		binary := a.cfg.Binary
		workspaceRoot := a.cfg.WorkspaceRoot
		projectID := a.cfg.ProjectID
		return a, func() tea.Msg {
			ctx, cancel := context.WithCancel(context.Background())
			client, err := rpcclient.Spawn(ctx, rpcclient.Config{
				Binary:        binary,
				WorkspaceRoot: workspaceRoot,
				ProjectID:     projectID,
			})
			return rpcSpawnedMsg{client: client, cancel: cancel, err: err}
		}

	case rpcSpawnedMsg:
		if m.err != nil {
			a.session.AppendMessage(state.Message{Role: state.RoleSystem, Text: "[error] failed to connect to core: " + m.err.Error()})
			a.chat.SetMessages(a.session.Messages)
			return a, nil
		}
		a.rpc = m.client
		a.rpcCancel = m.cancel
		return a, a.listenForEvents()

	case tea.KeyMsg:
		// Handle onboarding flow input first.
		if a.showOnboarding && a.onboarding != nil {
			return a.updateOnboarding(m)
		}
		// Handle command modal input.
		if a.commandModal != nil && a.commandModal.Active() {
			return a.updateCommandModal(m)
		}

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
				a.updateStatusHints()
				if a.rpc != nil {
					a.rpc.RespondPermission(true)
				}
				return a, nil
			}
		case "n":
			if a.permModal != nil {
				a.permModal = nil
				a.updateStatusHints()
				if a.rpc != nil {
					a.rpc.RespondPermission(false)
				}
				return a, nil
			}
		case "ctrl+c":
			return a, tea.Quit
		case "ctrl+k":
			if a.commandModal == nil {
				a.commandModal = view.NewPaletteModal(a.width, a.height)
			}
			a.commandModal.SetActive(true)
			return a, nil
		case "ctrl+o":
			if a.showOnboarding {
				return a, nil
			}
			if a.onboarding == nil {
				a.onboarding = view.NewOnboardingView(a.width, a.height)
			}
			a.onboarding.Step = view.OnboardingModel
			a.onboarding.LoadingModels = true
			a.showOnboarding = true
			endpoint := "http://localhost:1234"
			return a, fetchModelsCmd(endpoint)
		case "esc":
			if a.mentionActive {
				a.mentionActive = false
				a.layout()
				a.updateStatusHints()
				return a, nil
			}
			if a.paletteActive {
				a.paletteActive = false
				a.input.Reset()
				a.layout()
				a.updateStatusHints()
				return a, nil
			}
			if a.permModal != nil {
				a.permModal = nil
				a.updateStatusHints()
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
					a.updateStatusHints()
				}
				return a, nil
			}
			// Toggle last tool block.
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
				a.updateStatusHints()
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
				a.updateStatusHints()
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
				a.updateStatusHints()
				return a, nil
			}
			if a.paletteActive {
				selectedCmd := a.slashPalette.Selected()
				a.paletteActive = false
				a.input.Reset()
				a.layout()
				a.updateStatusHints()
				cmd := a.executePaletteCmd(selectedCmd)
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
				a.statusBar.SetAgentBusy(true)
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
		a.updateStatusHints()
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
		a.statusBar.SetAgentBusy(false)
		a.statusBar.ClearError()
		a.chat.SetStreamCursor(false)
	case rpcclient.EventError, rpcclient.EventConnectionError:
		a.agentBusy = false
		a.statusBar.SetAgentBusy(false)
		a.statusBar.SetError(ev.Err)
		a.chat.SetStreamCursor(false)
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
	a.updateStatusHints()
}

// View renders the full screen layout.
func (a *App) View() string {
	if a.width == 0 || a.height == 0 {
		return ""
	}

	// Onboarding overlays everything.
	if a.showOnboarding && a.onboarding != nil {
		a.onboarding.SetScreenSize(a.width, a.height)
		return a.onboarding.Render()
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
	parts = append(parts, a.statusBar.Render())

	screen := strings.Join(parts, "\n")

	// Command modal overlaid on top.
	if a.commandModal != nil && a.commandModal.Active() {
		a.commandModal.SetScreenSize(a.width, a.height)
		return a.commandModal.Render()
	}
	return screen
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
	a.statusBar.SetWidth(a.width)
	if a.commandModal != nil {
		a.commandModal.SetScreenSize(a.width, a.height)
	}

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
	// header(1) + statusbar(1) + input + actionBar + modal + palette
	chatHeight := a.height - 1 - 1 - inputRows - actionBarRows - modalRows - paletteRows
	if chatHeight < 1 {
		chatHeight = 1
	}

	if !a.initialized {
		a.chat = view.NewChat(a.width, chatHeight)
		a.chat.SetWelcomeInfo(a.buildWelcomeInfo())
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
		matches := fuzzy.Find(filepath.ToSlash(q), a.workspaceFiles)
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

// updateOnboarding handles keyboard input while the onboarding wizard is active.
func (a *App) updateOnboarding(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	ob := a.onboarding
	switch m.String() {
	case "ctrl+c":
		return a, tea.Quit
	case "esc":
		if ob.Step == view.OnboardingProvider {
			return a, nil // can't go back from first step
		}
		ob.Step--
		return a, nil
	case "up":
		switch ob.Step {
		case view.OnboardingProvider:
			ob.ProviderCursorUp()
		case view.OnboardingModel:
			ob.ModelCursorUp()
		case view.OnboardingSettings:
			ob.SettingsCursorUp()
		}
	case "down":
		switch ob.Step {
		case view.OnboardingProvider:
			ob.ProviderCursorDown()
		case view.OnboardingModel:
			ob.ModelCursorDown()
		case view.OnboardingSettings:
			ob.SettingsCursorDown()
		}
	case "left":
		if ob.Step == view.OnboardingSettings {
			ob.AdjustSetting(-1)
		}
	case "right":
		if ob.Step == view.OnboardingSettings {
			ob.AdjustSetting(+1)
		}
	case "enter":
		switch ob.Step {
		case view.OnboardingProvider:
			p := ob.SelectedProvider()
			endpoint := p.Endpoint
			if p.Name == "Custom" {
				endpoint = ob.CustomEndpoint
			}
			ob.Step = view.OnboardingModel
			ob.LoadingModels = true
			ob.ModelError = ""
			return a, fetchModelsCmd(endpoint)
		case view.OnboardingModel:
			if len(ob.Models) > 0 {
				sel := ob.SelectedModel()
				if sel.MaxContextLength > 0 {
					ob.Settings.NumCtx = sel.MaxContextLength
				}
				ob.Step = view.OnboardingSettings
			}
		case view.OnboardingSettings:
			return a, a.saveOnboardingConfig()
		}
	default:
		// Custom URL typing when editing custom endpoint.
		if ob.Step == view.OnboardingProvider && ob.IsEditingCustom() {
			if m.String() == "backspace" {
				ob.BackspaceCustomEndpoint()
			} else if len(m.Runes) == 1 {
				ob.TypeCustomEndpoint(m.Runes[0])
			}
		}
	}
	return a, nil
}

// saveOnboardingConfig writes the selected model and settings to .orchestra.yml.
func (a *App) saveOnboardingConfig() tea.Cmd {
	ob := a.onboarding
	sel := ob.SelectedModel()
	p := ob.SelectedProvider()
	endpoint := p.Endpoint
	if p.Name == "Custom" {
		endpoint = ob.CustomEndpoint
	}
	cfgPath := a.cfg.ConfigPath
	workspaceRoot := a.cfg.WorkspaceRoot

	return func() tea.Msg {
		cfg, err := config.Load(cfgPath)
		if err != nil {
			cfg = config.DefaultConfig(workspaceRoot)
			cfg.ProjectRoot = workspaceRoot
		}
		cfg.LLM.APIBase = endpoint
		cfg.LLM.Model = sel.ID
		cfg.LLM.Temperature = ob.Settings.Temperature
		cfg.LLM.MaxTokens = ob.Settings.MaxTokens
		if cfg.LLM.ExtraBody == nil {
			cfg.LLM.ExtraBody = map[string]any{}
		}
		cfg.LLM.ExtraBody["num_ctx"] = ob.Settings.NumCtx
		if ob.Settings.EnableThinking {
			cfg.LLM.ExtraBody["chat_template_kwargs"] = map[string]any{"enable_thinking": true}
		} else {
			delete(cfg.LLM.ExtraBody, "chat_template_kwargs")
		}
		if err := config.Save(cfgPath, cfg); err != nil {
			return modelsLoadedMsg{err: fmt.Errorf("save config: %w", err)}
		}
		return onboardingDoneMsg{configPath: cfgPath}
	}
}

// updateCommandModal handles keyboard input while the Ctrl+K modal is open.
func (a *App) updateCommandModal(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	cm := a.commandModal
	switch m.String() {
	case "ctrl+c":
		return a, tea.Quit
	case "esc":
		cm.SetActive(false)
	case "up":
		cm.CursorUp()
	case "down":
		cm.CursorDown()
	case "enter":
		selected := cm.Selected()
		cm.SetActive(false)
		if selected != "" {
			cmd := a.executePaletteCmd(selected)
			return a, cmd
		}
	case "backspace":
		cm.Backspace()
	default:
		if len(m.Runes) == 1 {
			cm.TypeRune(m.Runes[0])
		}
	}
	return a, nil
}

const helpText = `Orchestra TUI — key bindings:
  Enter         send message
  Shift+Enter   newline in input
  Tab           expand/collapse last tool block
  ↑ / ↓         input history (single-line mode)
  /             slash command palette
  @             file mention (fuzzy)
  Ctrl+K        command palette modal
  Ctrl+O        change model (onboarding)
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

// buildWelcomeInfo constructs the metadata shown on the welcome screen.
func (a *App) buildWelcomeInfo() view.WelcomeInfo {
	projectPath := a.cfg.WorkspaceRoot
	projectName := a.cfg.CWD
	if projectName == "" && projectPath != "" {
		projectName = filepath.Base(projectPath)
	}
	return view.WelcomeInfo{
		ProjectPath:  projectPath,
		ProjectName:  projectName,
		ModelName:    a.cfg.Model,
		SessionCount: countSessions(projectPath),
	}
}

// countSessions returns the number of past agent sessions in this workspace.
// It looks for JSONL run files in the .orchestra/ directory.
func countSessions(workspaceRoot string) int {
	orchDir := filepath.Join(workspaceRoot, ".orchestra")
	entries, err := os.ReadDir(orchDir)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			info, err := e.Info()
			if err == nil && info.Size() > 0 {
				count++
			}
		}
	}
	return count
}

// updateStatusHints updates the status bar hint text based on the current UI state.
func (a *App) updateStatusHints() {
	switch {
	case a.paletteActive:
		a.statusBar.SetHints("↑↓ select · Enter execute · Esc cancel")
	case a.mentionActive:
		a.statusBar.SetHints("↑↓ select · Enter/Tab insert · Esc cancel")
	case a.permModal != nil:
		a.statusBar.SetHints("[y]es allow · [n]o deny · Esc deny")
	case a.pendingOps != nil:
		a.statusBar.SetHints("[a]pply · [d]iff · [x]discard · Ctrl+C quit")
	default:
		a.statusBar.SetHints("/ commands")
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
