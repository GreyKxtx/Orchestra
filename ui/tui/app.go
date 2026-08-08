// Package tui implements the Orchestra terminal UI client.
//
// The code is split into focused files for maintainability:
//
//	app.go              — Config, App struct, tea.Msg types, NewApp, Init, Run
//	app_update.go       — Update dispatcher
//	app_keys.go         — routeKey / Enter / selection / scroll
//	app_chrome_keys.go  — Ctrl+T/R, bare d/t, chrome cascade
//	app_mouse.go        — mouse handlers
//	app_layout.go       — layout()
//	app_diff.go         — commit diff toggle / restore
//	app_status.go       — chromeMetrics sync + prefs
//	app_view.go         — View + input chrome + hints
//	app_rpc.go          — RPC stream handlers
//	app_dialogs.go      — dialog stack
//	app_palette.go      — slash/mention palettes
//	app_onboarding.go   — provider/model wizard
//	app_session.go      — session persist/load
//	app_todos/turn/…    — chrome helpers
package tui

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/internal/llm"
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
	AllowExec       bool   // allow bash/exec.run in TUI agent runs
	Profile         string // agent.profile: fast | precision | ""
}

// App is the root Bubble Tea Model.
type App struct {
	cfg       Config
	session   *state.Session
	chat      view.Chat
	input     view.Input
	statusBar view.StatusBar

	taskPanel     *view.TaskPanel
	todos         []rpcclient.TodoItem

	msgQueue []string // FIFO prompts submitted while agent busy

	width       int
	height      int
	initialized bool

	rpc       *rpcclient.Client
	rpcCancel context.CancelFunc

	lastCommitDiff []rpcclient.FileDiff // diff from last auto-commit (for /diff)
	diffShown      bool                 // true while diff messages are in session
	diffCursor     int                  // selected file index in expanded diff review

	pendingOps    []map[string]any // dry-run ops awaiting user apply
	pendingReview bool             // true when pendingOps must be confirmed
	turn           *state.TurnFSM       // turn lifecycle FSM (M3)
	turnError      string

	chrome chromeMetrics

	permModal     *view.Modal         // non-nil while an exec.run permission request is pending
	questionModal *view.QuestionModal // non-nil while question/ask RPC is pending

	slashPalette  *view.SlashPalette
	paletteActive bool

	mentionPalette *view.MentionPalette
	mentionActive  bool
	workspaceFiles []string // lazily populated on first @-mention

	// workflowProgress renders a sticky one-line widget above the input that
	// shows per-stage status while a workflow.run is in flight. Driven by
	// EventWorkflowStageStart/Done events; cleared on workflowResultMsg.
	workflowProgress *view.WorkflowProgress

	// activeCancel cancels the currently in-flight long-running RPC
	// (agent.run / workflow.run / skill.invoke). Set when the call starts,
	// cleared when it completes. Bound to Esc while the turn is busy so the user
	// can abort a stuck run; the rpcclient's Call sends $/cancelRequest to
	// the core so the server-side ctx is cancelled too.
	activeCancel context.CancelFunc

	history *state.InputHistory

	commandModal   *view.PaletteModal   // Ctrl+K modal
	onboarding     *view.OnboardingView // 3-step wizard
	showOnboarding bool
	showWelcome    bool      // true on every startup until user sends first message
	cursorBlink    bool      // toggles every ~500ms while turn is running/applying
	spinFrame      int       // monotonically increments every tick — drives all spinners
	turnStartedAt  time.Time // moment the current agent.run was kicked off

	coreSessionID string // JSON-RPC session for multi-turn agent history

	// dialogStack holds dialogs opened from the Ctrl+K palette
	// (/provider → ProviderDialog → ModelDialog → SettingsDialog).
	// Top of stack receives input and is rendered on top of everything else.
	dialogStack []view.Dialog

	// orchFlow is set while nested provider/model dialogs edit an Orchestra role
	// (must not save into global llm: / settings).
	orchFlow      bool
	orchRoleIdx   int
	orchPending   string // provider key while picking model; "" = main
	orchPendingP  view.ProviderEntry
	pendingAPIKey string // from EndpointDialog; used for /v1/models fetch

	// reasoning routes streamed assistant text into reasoning vs message
	// buffers based on `<think>...</think>` tags. State persists across
	// deltas so a tag straddling two chunks is still recognized.
	reasoning state.ReasoningSplitter

	// stepTextLen tracks the byte length of the active assistant's Text at
	// the start of the current LLM step. On EventStepDone("tool_call") the
	// text is truncated back to this value to discard pre-tool-call chatter.
	stepTextLen int

	// retryHintThisStep suppresses duplicate generic retry lines when
	// EventRecoverableError already showed the detailed hint this step.
	retryHintThisStep bool

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
	inputRowY         int  // absolute screen row of textarea content
	inputBoxTopY      int  // absolute screen row of input box top (chat mode)
	inputColX         int  // absolute screen column where textarea content starts
	chatTopY          int  // absolute screen row of the first viewport content line
	statusBarRowY     int  // status bar content row (tasks chip click target)
	mouseDown         bool // true while left button held in input area
	mouseLastClickAt  time.Time
	mouseLastClickPos int
	mouseClickCount   int

	// mousePassthrough disables mouse reporting so the terminal can handle
	// native text selection (toggled with Ctrl+G).
	mousePassthrough bool

	// pasteBurstUntil marks the end of an in-progress terminal paste. While
	// active, Enter/Space are inserted as text (not submit / word-break), so
	// multi-line clipboard content is not truncated mid-paste.
	pasteBurstUntil time.Time
	lastRuneAt      time.Time
	// floodRunCount counts consecutive sub-pasteFloodGap single runes; a
	// paste burst arms only after pasteFloodArmCount of them (fast typing
	// bursts must not swallow the following Enter).
	floodRunCount int

	allowExec bool // allow bash/exec.run in agent runs

	// routeBadge is set when mode=agent auto-routes (e.g. "agent→build").
	routeBadge string

	// sessionToolAllow remembers tools approved with [t] for this TUI session
	// (ask mode still on; these tools skip the permission modal).
	sessionToolAllow map[string]bool

	// stickyTasksTopY / stickyTasksHeight track the pinned checklist for layout.
	stickyTasksTopY   int
	stickyTasksHeight int
}

// rpcEventMsg wraps an rpcclient.Event for the Bubble Tea event loop.
type rpcEventMsg rpcclient.Event

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
	provider  view.ProviderEntry
	model     view.ModelEntry
	numCtx    int64
	maxTokens int
	err       error
}

// llmProbeMsg is the result of an async LLM connectivity check.
type llmProbeMsg struct {
	phase    string // "endpoint" | "settings" | "startup"
	provider view.ProviderEntry
	apiKey   string
	result   llm.ProbeResult
}

// limitsAppliedMsg reports that server-discovered context was reconciled into config.
type limitsAppliedMsg struct {
	contextTokens int  // effective num_ctx after reconcile
	serverMax     int  // raw max_model_len from probe
	maxTokens     int
	clamped       bool // max_tokens reduced
	ctxClamped    bool // user num_ctx reduced to server max
	err           error
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
		cfg:              cfg,
		session:          state.NewSession(),
		turn:             state.NewTurnFSM(),
		allowExec:        cfg.AllowExec,
		sessionToolAllow: map[string]bool{},
	}
	a.taskPanel = view.NewTaskPanel(0) // tests only; sticky checklist is live UI
	a.loadConfigPrefs()
	a.syncStatusBar()
	a.statusBar.SetModel(cfg.Model)
	a.statusBar.SetProject(cfg.CWD)
	a.showWelcome = true
	a.slashPalette = view.NewSlashPalette(0)         // width set in layout()
	a.mentionPalette = view.NewMentionPalette(0)     // width set in layout()
	a.workflowProgress = view.NewWorkflowProgress(0) // width set in layout()
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
	cmds := []tea.Cmd{textarea.Blink, tea.EnableBracketedPaste, a.listenForEvents(), tickCmd()}
	if a.rpc != nil {
		cmds = append(cmds, a.startCoreSession())
	}
	return tea.Batch(cmds...)
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
		client := lmstudio.NewClient(endpoint, "")
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
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	return err
}
