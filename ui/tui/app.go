// Package tui implements the Orchestra terminal UI client.
//
// The code is split into focused files for maintainability:
//
//	app.go            — Config, App struct, tea.Msg types, NewApp, Init,
//	                    tick/fetch cmds, Run entry-point.
//	app_update.go     — Update + routeKey + handleEnter.
//	app_view.go       — View dispatcher, layout, renderInputBox, renderThinkingLine,
//	                    cycleAgentMode, updateStatusHints.
//	app_welcome.go    — welcome view + welcomeModeLine + bottom bar.
//	app_rpc.go        — RPC stream handlers, <think>...</think> splitter.
//	app_dialogs.go    — dialog stack + handleDialogResult + settings/respawn.
//	app_palette.go    — slash/mention palettes, command modal, executePaletteCmd.
//	app_onboarding.go — provider/model/settings wizard.
//	app_session.go    — session record persist/load.
package tui

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/internal/lmstudio"
	"github.com/orchestra/orchestra/ui/tui/rpcclient"
	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/theme"
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
	Theme           string // registered theme name; empty → default
}

// App is the root Bubble Tea Model.
type App struct {
	cfg       Config
	session   *state.Session
	chat      view.Chat
	input     view.Input
	statusBar view.StatusBar // replaces footer

	width       int
	height      int
	initialized bool

	rpc       *rpcclient.Client
	rpcCancel context.CancelFunc

	pendingOps *rpcclient.PendingOpsPayload // non-nil while ops await confirmation
	diffShown  bool                         // true while diff messages are in session
	agentBusy  bool                         // true while agent.run in flight

	permModal *view.Modal // non-nil while an exec.run permission request is pending

	slashPalette  *view.SlashPalette
	paletteActive bool

	mentionPalette *view.MentionPalette
	mentionActive  bool
	workspaceFiles []string // lazily populated on first @-mention

	history *state.InputHistory

	commandModal   *view.PaletteModal   // Ctrl+K modal
	onboarding     *view.OnboardingView // 3-step wizard
	showOnboarding bool
	showWelcome    bool      // true on every startup until user sends first message
	cursorBlink    bool      // toggles every ~500ms while agentBusy
	spinFrame      int       // monotonically increments every tick — drives all spinners
	turnStartedAt  time.Time // moment the current agent.run was kicked off

	// dialogStack holds dialogs opened from the Ctrl+K palette
	// (/provider → ProviderDialog → ModelDialog → SettingsDialog).
	// Top of stack receives input and is rendered on top of everything else.
	dialogStack []view.Dialog

	// reasoning routes streamed assistant text into reasoning vs message
	// buffers based on `<think>...</think>` tags. State persists across
	// deltas so a tag straddling two chunks is still recognized.
	reasoning state.ReasoningSplitter

	// currentSessionID is the on-disk id of the in-flight chat. Empty until
	// the first user message is sent (and the session record is created).
	currentSessionID string

	// chatDirty is set by handleRPCEvent whenever a streaming delta has been
	// recorded into the session, and cleared by the next tick that flushes
	// it to chat.SetMessages. Lets us avoid full re-renders on quiet ticks.
	chatDirty bool

	toastText string // non-empty while toast is visible
	toastTick int    // countdown ticks until toast clears

	// Mouse state for click-to-cursor and drag selection.
	inputRowY int  // absolute screen row of textarea content
	inputColX int  // absolute screen column where textarea content starts
	mouseDown         bool // true while left button held in input area
	mouseLastClickAt  time.Time
	mouseLastClickPos int
	mouseClickCount   int

	// Sticky desired column for vertical (Up/Down) navigation. -1 means
	// "not in a vertical nav sequence" — the next Up/Down captures the
	// current visual column. Reset to -1 on any non-vertical action so
	// the sequence ends and a fresh column is captured next time.
	lastVisualCol int
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

// settingsSavedMsg is emitted by the dialog flow when a SettingsDialog
// completes successfully and the new config has been written to disk.
type settingsSavedMsg struct {
	provider view.ProviderEntry
	model    view.ModelEntry
	err      error
}

// NewApp constructs an App with the given config. If cfg.Binary is non-empty,
// spawns the core subprocess and runs the initialize handshake; on error,
// returns it.
func NewApp(cfg Config) (*App, error) {
	// Apply theme as early as possible so every styled component built below
	// reads from the right palette.
	if cfg.Theme != "" {
		theme.SetTheme(theme.ByName(cfg.Theme))
	}
	a := &App{
		cfg:           cfg,
		session:       state.NewSession(),
		lastVisualCol: -1,
	}
	a.statusBar.SetModel(cfg.Model)
	a.statusBar.SetProject(cfg.CWD)
	a.showWelcome = true
	a.slashPalette = view.NewSlashPalette(0)     // width set in layout()
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

// tickCmd schedules the next tick at 100ms — fast enough for a smooth spinner
// (10 fps). Cursor blink toggles every 5 ticks (≈500ms) so the textarea cursor
// behavior stays the same.
func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
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
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseAllMotion())
	_, err = p.Run()
	return err
}
