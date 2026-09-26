package core

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/app"
	"github.com/orchestra/orchestra/internal/autorouter"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/contract"
	"github.com/orchestra/orchestra/internal/hooks"
	"github.com/orchestra/orchestra/internal/orchestrastate"
	"github.com/orchestra/orchestra/internal/permission"
	"github.com/orchestra/orchestra/internal/skillrun"
	"github.com/orchestra/orchestra/internal/skills"
	"github.com/orchestra/orchestra/internal/tasks"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/internal/trajectory"
	"github.com/orchestra/orchestra/internal/usage"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/protocol/wire"
)

// agentLaunchSpec is the shared input for building agent.Options from Core config.
// AgentRun and SessionMessage both go through prepareAgentLaunch so new knobs
// (digest, prune, memory, …) cannot drift between entry points.
type agentLaunchSpec struct {
	Mode      string
	Profile   string
	PlanPath  string
	SessionID string
	// RecordRun keeps a trajectory for a turn with no session: agent.run's
	// log goes to .orchestra/runs/<turn_id>.events.jsonl.
	RecordRun bool
	// OnGraphChange follows the turn's task graph (the run's checkpoint).
	OnGraphChange func()
	Query         string // user turn text; used by mode=agent auto-router

	Apply     bool
	Backup    bool
	AllowExec bool
	// AllowWeb is web consent for this turn beside web.confirm: false (a run
	// in-process: --allow-web).
	AllowWeb     bool
	AllowBrowser bool
	Debug        bool

	MaxSteps          int
	MaxInvalidRetries int
	MaxPromptBytes    int

	InitialTodos []tools.TodoItem

	AutoSessionMemory bool // false for one-shot agent.run; config-resolved for sessions
	UsageLabel        string

	OnEvent func(method string, params any)
	// OnAgentEvent receives the top-level agent's events as they are (a run
	// in-process renders them), beside the notifications OnEvent gets.
	OnAgentEvent        func(agent.AgentEvent)
	EventEnvelope       EventEnvelope
	PermissionRequester PermissionRequester
	QuestionAsker       tools.QuestionAsker

	Attachments []MessageAttachment
	UserImages  []llm.ContentPart
	Multimodal  bool
}

type agentLaunch struct {
	Opts       agent.Options
	Custom     customAgentOpts
	Usage      *usage.Tracker
	TaskRunner *tasks.TaskRunner
	Profile    string

	RequestedMode   string
	EffectiveMode   string
	RouteReason     string
	RouteConfidence float64
	EventEnvelope   EventEnvelope

	// Trajectory records this turn's notifications: in the session's log, or
	// for a one-shot agent.run in .orchestra/runs/<turn_id>.events.jsonl. Nil
	// when there is nothing to record against, or when the log could not be
	// opened; Close is safe either way.
	Trajectory *trajectory.Writer

	// tools is the runner whose per-run memos the launch clears when it
	// closes.
	tools *tools.Runner

	// turnStartedAt is when the boundary was recorded, so Close can say how
	// long the turn took rather than leaving a reader to subtract timestamps
	// of whatever events happened to bracket it.
	turnStartedAt time.Time
	sessionID     string
}

// RunContext returns ctx attributed to this turn (llm.Trace): the model calls
// and llm_log lines of the turn carry its turn_id as run_id, and its subagent
// tasks add their own identity to it (tasks.ChildAgentConfig.RunID).
func (l *agentLaunch) RunContext(ctx context.Context) context.Context {
	if l == nil {
		return ctx
	}
	// Consent prompts raised under the turn — an exec.run, a language server
	// to install — go to this turn's client.
	ctx = permission.WithRequester(ctx, l.Opts.PermissionRequester)
	if l.EventEnvelope.TurnID == "" {
		return ctx
	}
	return llm.WithTrace(ctx, llm.Trace{RunID: l.EventEnvelope.TurnID})
}

// Close releases what the launch holds open, and records where the turn ended.
// Callers own the turn, so they own this: prepareAgentLaunch returns before
// the first event exists and cannot defer it itself.
func (l *agentLaunch) Close() {
	if l == nil {
		return
	}
	// Children nobody waited for — a task_spawn the turn ended without
	// collecting, workers relayed from a Lead's batch — stop with the turn
	// instead of editing the workspace after it has been reported done.
	l.TaskRunner.Close()
	// The run's agents are gone with it; what they were given (a directory's
	// ORCHESTRA.md) is forgotten so the memo does not carry every run the
	// core ever served.
	if l.tools != nil {
		l.tools.ForgetInstructionsOf(l.EventEnvelope.TurnID)
	}
	if l.Trajectory == nil {
		return
	}
	// Best-effort, like every other write to the log: a boundary that cannot
	// be recorded must not fail a turn that has already run.
	dur := int64(0)
	if !l.turnStartedAt.IsZero() {
		dur = time.Since(l.turnStartedAt).Milliseconds()
	}
	_ = l.Trajectory.Append(trajectory.TypeTurnEnd, map[string]any{
		"turn_id":     l.EventEnvelope.TurnID,
		"session_id":  l.sessionID,
		"duration_ms": dur,
	})
	_ = l.Trajectory.Close()
}

// resolveApplyOutput normalises apply_output and forces dry-run for patch mode.
func resolveApplyOutput(cfg *config.ProjectConfig, applyOutput string, apply *bool, backup *bool) (string, error) {
	out := strings.ToLower(strings.TrimSpace(applyOutput))
	if out == "" && cfg != nil {
		out = strings.ToLower(strings.TrimSpace(cfg.Apply.Output))
	}
	if out == "" {
		out = config.ApplyOutputDisk
	}
	if out != config.ApplyOutputDisk && out != config.ApplyOutputPatch {
		return "", protocol.NewError(protocol.InvalidParams,
			fmt.Sprintf("apply_output must be %q or %q", config.ApplyOutputDisk, config.ApplyOutputPatch), nil)
	}
	if out == config.ApplyOutputPatch {
		if apply != nil && *apply {
			return "", protocol.NewError(protocol.InvalidParams,
				"apply_output=patch is mutually exclusive with apply=true", nil)
		}
		if apply != nil {
			*apply = false
		}
		if backup != nil {
			*backup = false
		}
	}
	return out, nil
}

func resolveProfileName(cfg *config.ProjectConfig, profile string) (string, error) {
	name := strings.TrimSpace(profile)
	if name == "" && cfg != nil {
		name = strings.TrimSpace(cfg.Agent.Profile)
	}
	if !agent.IsKnownProfile(name) {
		return "", protocol.NewError(protocol.InvalidParams,
			fmt.Sprintf("unknown profile %q (want fast|precision)", name), nil)
	}
	return name, nil
}

// turnPrep is what prepareAgentLaunch resolves on the way to the turn's
// options: its consents and budget, its recorder and event sink, its route,
// its client and tools, its task runner.
type turnPrep struct {
	spec     agentLaunchSpec
	settings app.Settings
	profile  string
	env      EventEnvelope

	allowExec, allowWeb, allowBrowser    bool
	maxSteps, maxRetries, maxPromptBytes int

	trajectory  *trajectory.Writer
	onEvent     func(agent.AgentEvent)
	agentLogger *llm.Logger
	hooks       agent.HooksRunner

	requestedMode, effectiveMode string
	routeReason                  string
	routeConfidence              float64

	custom     customAgentOpts
	usage      *usage.Tracker
	childCfg   tasks.ChildAgentConfig
	taskRunner *tasks.TaskRunner
}

func (c *Core) prepareAgentLaunch(ctx context.Context, spec agentLaunchSpec) (launch *agentLaunch, retErr error) {
	if c == nil || c.cfg == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core config is nil", nil)
	}
	// LSP auto-provision for this turn asks this turn's client. Warmup is
	// best-effort and must not block the agent loop on npm/go install.
	if c.tools != nil {
		c.WarmupLSP(permission.WithRequester(context.Background(), spec.PermissionRequester))
	}
	profileName, err := resolveProfileName(c.cfg, spec.Profile)
	if err != nil {
		return nil, err
	}
	p := c.newTurnPrep(spec, profileName)
	// The mode router below calls the model on the turn's behalf.
	ctx = llm.WithTrace(ctx, llm.Trace{RunID: p.env.TurnID})
	c.recordTurn(p)
	// The launch owns the writer once it exists, and its three callers defer
	// Close. Between here and that construction sit error returns, and a
	// writer abandoned there would leak its handle: on Windows an open handle
	// makes the sidecar undeletable, and sessionfile.Delete removes the
	// snapshot before the sidecar, so the session would half-vanish and leave
	// an orphan behind. Closing on the error path only, via a named return,
	// keeps that true for error paths added later — patching the two that
	// exist today would not.
	defer func() {
		if retErr != nil && p.trajectory != nil {
			_ = p.trajectory.Close()
		}
	}()
	p.onEvent = turnEventSink(p.spec, p.env)
	c.routeTurn(ctx, p)
	if err := c.resolveTurnAgent(p); err != nil {
		return nil, err
	}
	c.newTurnTaskRunner(p)
	opts, err := c.turnOptions(p)
	if err != nil {
		return nil, protocol.NewError(protocol.InvalidParams, err.Error(), nil)
	}
	return &agentLaunch{
		Opts:            opts,
		Custom:          p.custom,
		Usage:           p.usage,
		TaskRunner:      p.taskRunner,
		Profile:         p.profile,
		RequestedMode:   p.requestedMode,
		EffectiveMode:   p.effectiveMode,
		RouteReason:     p.routeReason,
		RouteConfidence: p.routeConfidence,
		EventEnvelope:   p.env,
		Trajectory:      p.trajectory,
		tools:           c.tools,
		turnStartedAt:   time.Now(),
		sessionID:       spec.SessionID,
	}, nil
}

// newTurnPrep resolves what the turn may do and how much of it there may
// be — one answer for the turn, its subagents and its skills — and where
// its own lines go.
func (c *Core) newTurnPrep(spec agentLaunchSpec, profile string) *turnPrep {
	p := &turnPrep{spec: spec, settings: app.SettingsFrom(c.cfg), profile: profile, env: spec.EventEnvelope}
	if p.env.TurnID == "" {
		p.env.TurnID = NewTurnID()
	}
	p.allowBrowser = spec.AllowBrowser && agent.ProfileAllowsBrowser(profile)
	p.allowExec = spec.AllowExec
	if c.cfg.Exec.Confirm != nil && !*c.cfg.Exec.Confirm {
		p.allowExec = true
	}
	// Web consent is web.confirm: false, or the consent a run in-process was
	// started with; the turn, its skills and its subagents all take it from
	// here.
	p.allowWeb = spec.AllowWeb || (c.cfg.Web.Confirm != nil && !*c.cfg.Web.Confirm)
	p.maxSteps = spec.MaxSteps
	if p.maxSteps <= 0 {
		p.maxSteps = p.settings.MaxSteps
	}
	p.maxRetries = spec.MaxInvalidRetries
	if p.maxRetries <= 0 {
		p.maxRetries = p.settings.MaxInvalidRetries
	}
	p.maxPromptBytes = spec.MaxPromptBytes
	if p.maxPromptBytes <= 0 {
		p.maxPromptBytes = p.settings.MaxPromptBytes
	}
	// The agent's own lines (tool calls, results, classifications) go to
	// llm_log.jsonl whatever the provider: the client's logger when it has
	// one, a fresh handle on the same file otherwise.
	p.agentLogger = llm.LoggerOf(c.llmClient)
	if p.agentLogger == nil {
		p.agentLogger = llm.NewLogger(c.workspaceRoot)
	}
	if hr := hooks.New(c.cfg.Hooks, c.workspaceRoot).WithSession(spec.SessionID); hr != nil {
		p.hooks = hr
	}
	return p
}

// recordTurn opens the turn's trajectory — the session's log, or for a
// one-shot agent.run the run's — and tees every notification into it: one
// tee for every consumer of spec.OnEvent (there are four, and wrapping them
// individually would drop whichever one a later change adds). Observability
// never blocks work: a log that cannot be opened leaves the turn unrecorded.
func (c *Core) recordTurn(p *turnPrep) {
	if p.spec.SessionID == "" && !p.spec.RecordRun {
		return
	}
	var w *trajectory.Writer
	var err error
	if p.spec.SessionID != "" {
		w, err = trajectory.NewWriter(c.workspaceRoot, p.spec.SessionID)
	} else {
		w, err = trajectory.NewRunWriter(c.workspaceRoot, p.env.TurnID)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "core: turn %s trajectory recording disabled: %v\n", p.env.TurnID, err)
		return
	}
	p.trajectory = w
	p.spec.OnEvent = teeToTrajectory(p.spec.OnEvent, w)
	// The first line of the turn, written before the agent can emit
	// anything, so a turn that produces no notifications at all is still
	// visible as a turn that ran.
	_ = w.Append(trajectory.TypeTurnStart, map[string]any{
		"turn_id":    p.env.TurnID,
		"session_id": p.spec.SessionID,
	})
}

// turnEventSink is where the top-level agent's events go: the client's
// notifications, and beside them the direct sink a run in-process renders
// from.
func turnEventSink(spec agentLaunchSpec, env EventEnvelope) func(agent.AgentEvent) {
	var onEvent func(agent.AgentEvent)
	if spec.OnEvent != nil {
		onEvent = buildAgentOnEvent(spec.OnEvent, env)
	}
	if direct := spec.OnAgentEvent; direct != nil {
		if notify := onEvent; notify != nil {
			onEvent = func(ev agent.AgentEvent) {
				direct(ev)
				notify(ev)
			}
		} else {
			onEvent = direct
		}
	}
	return onEvent
}

// routeTurn settles the turn's mode: the one asked for, or for mode=agent
// the one the router picks for the query — kept off a mode without browser
// tools when the browser is on. The client hears of a routed turn.
func (c *Core) routeTurn(ctx context.Context, p *turnPrep) {
	p.requestedMode = strings.TrimSpace(p.spec.Mode)
	p.effectiveMode = p.requestedMode
	if !strings.EqualFold(p.requestedMode, string(agent.ModeAgent)) {
		return
	}
	dec := c.classifyAgentMode(ctx, p.spec.Query, p.agentLogger)
	p.effectiveMode, p.routeReason, p.routeConfidence = dec.Mode, dec.Reason, dec.Confidence
	if mode, kept := agent.ModeForRoutedTurn(p.effectiveMode, p.allowBrowser); kept {
		p.routeReason = fmt.Sprintf("the browser is on and %s mode has no browser tools; router: %s", p.effectiveMode, p.routeReason)
		p.effectiveMode = mode
	}
	if p.spec.OnEvent != nil {
		p.spec.OnEvent(wire.NotifyAgentEvent, p.env.stamp(wire.AgentEvent{
			Type: wire.EventModeRoute,
			Data: wire.ModeRoute{
				From:       string(agent.ModeAgent),
				To:         p.effectiveMode,
				Reason:     p.routeReason,
				Confidence: p.routeConfidence,
			},
		}, nil))
	}
}

// resolveTurnAgent is the client and the tools the turn runs with: a custom
// agent's when the mode names one, the orchestra planner's for a Lead when
// one is configured.
func (c *Core) resolveTurnAgent(p *turnPrep) error {
	custom, err := c.resolveCustomAgentOpts(p.effectiveMode, tools.Capabilities{Exec: p.allowExec, Web: p.allowWeb, Browser: p.allowBrowser}, p.agentLogger)
	if err != nil {
		return protocol.NewError(protocol.InvalidLLMOutput, err.Error(), nil)
	}
	if strings.EqualFold(p.effectiveMode, string(agent.ModeOrchestra)) {
		if client, _, _, ok := c.resolveOrchestraPlanner(p.agentLogger); ok {
			custom.llmClient = client
		}
	}
	p.custom = custom
	return nil
}

// newTurnTaskRunner is the runner of the turn's subtasks. Subagents take the
// turn's breakers and permission rules — a deny rule is the project's, not
// the top-level agent's alone — and get the browser and the web when the
// turn has them; the agent refuses browser.* to any run without it, children
// included. The Question Barrier (spec §4.3) shares the interactive channel
// with the question tool; nil (core stdio mode) keeps the barrier off.
func (c *Core) newTurnTaskRunner(p *turnPrep) {
	usageLabel := p.spec.UsageLabel
	if usageLabel == "" {
		usageLabel = "agent.run"
	}
	p.usage = newAgentUsageTracker(c.cfg, usageLabel)
	cfg := c.buildChildAgentConfig(p.maxPromptBytes, p.usage, p.allowExec, p.agentLogger)
	cfg.Settings = &p.settings
	cfg.Agency, cfg.Agents = tasks.AgencyFromConfig(c.cfg, p.effectiveMode)
	cfg.RunID = p.env.TurnID
	cfg.Budget = tasks.BudgetFromConfig(c.cfg.Agent.TurnBudget)
	cfg.Caps.Browser = p.allowBrowser
	cfg.Caps.Web = p.allowWeb
	cfg.QuestionAsker = p.spec.QuestionAsker
	cfg.OnGraphChange = p.spec.OnGraphChange
	if notify := p.spec.OnEvent; notify != nil {
		env := p.env
		cfg.NotifyAgentEvent = func(ev wire.AgentEvent) {
			notify(wire.NotifyAgentEvent, env.stamp(ev, nil))
		}
		cfg.ChildEventSink = func(taskID, parentToolCallID, subagentType string) func(agent.AgentEvent) {
			meta := &ChildScopeMeta{
				TaskID:           taskID,
				ParentToolCallID: parentToolCallID,
				SubagentType:     subagentType,
			}
			return buildAgentOnEventWithChild(notify, env, meta)
		}
	}
	p.childCfg = cfg
	p.taskRunner = tasks.New(p.custom.llmClient, c.validator, c.tools, cfg)
}

// turnOptions builds the turn's agent.Options through the composition root.
func (c *Core) turnOptions(p *turnPrep) (agent.Options, error) {
	planPath := strings.TrimSpace(p.spec.PlanPath)
	if planPath == "" {
		planPath = resolvePlanPath(p.effectiveMode, "", "")
	}
	providerLabel, modelLabel := c.turnLabels(p.effectiveMode)
	respFmt := agent.ResolveResponseFormat(c.cfg.LLM, providerLabelOf(c.cfg), agent.ResponseFormatToolAgent)
	requester := convertPermissionRequester(p.spec.PermissionRequester)
	return app.TurnOptions(p.settings, p.profile, func(o *agent.Options) {
		o.MaxSteps = p.maxSteps
		o.MaxInvalidRetries = p.maxRetries
		o.MaxPromptBytes = p.maxPromptBytes
		o.Apply = p.spec.Apply
		o.Backup = p.spec.Backup
		o.AllowExec = p.allowExec
		o.AllowWeb = p.allowWeb
		o.AllowBrowser = p.allowBrowser
		o.InitialTodos = p.spec.InitialTodos
		o.Debug = p.spec.Debug
		o.ResponseFormat = respFmt
		o.Mode = agent.Mode(p.effectiveMode)
		o.SystemPromptOverride = p.custom.systemPromptOverride
		o.CustomTools = p.custom.customTools
		o.OnEvent = p.onEvent
		o.AgentLogger = p.agentLogger
		o.SubtaskRunner = p.taskRunner
		o.HooksRunner = p.hooks
		o.ExtraTools = c.extraToolDefs()
		o.PermissionRequester = requester
		o.QuestionAsker = p.spec.QuestionAsker
		o.UsageTracker = p.usage
		o.ProviderLabel = providerLabel
		o.ModelLabel = modelLabel
		o.PlanPath = planPath
		o.SessionID = p.spec.SessionID
		o.AutoSessionMemory = p.spec.AutoSessionMemory
		// Skills in TUI/core (parity with `orchestra apply`). Skip for
		// read-only / plan-only modes so skill_invoke cannot bypass write
		// guards via a child.
		if skillsAllowedInMode(p.effectiveMode) {
			o.Skills, o.SkillRunner = c.turnSkills(p, requester)
		}
		if cc, ctxTok := c.compactionClientWithContext(p.agentLogger); cc != nil {
			o.CompactionClient = cc
			o.CompactionContextTokens = ctxTok
		}
		if len(p.spec.UserImages) > 0 {
			o.UserImages = p.spec.UserImages
		}
		if p.spec.Multimodal || (c.cfg.LLM.Multimodal && len(p.spec.UserImages) > 0) {
			o.MultimodalLLM = c.cfg.LLM.Multimodal
		}
	})
}

// turnLabels names the provider and the model the turn's prompt is written
// for: the orchestra planner's for a Lead, when configured.
func (c *Core) turnLabels(mode string) (provider, model string) {
	provider, model = providerLabelOf(c.cfg), c.cfg.LLM.Model
	if strings.EqualFold(mode, string(agent.ModeOrchestra)) {
		if p := strings.TrimSpace(c.cfg.Orchestra.Planner.Provider); p != "" {
			provider = p
		}
		if m := strings.TrimSpace(c.cfg.Orchestra.Planner.Model); m != "" {
			model = m
		}
	}
	return provider, model
}

// turnSkills is the turn's file-based skills and the runner that invokes
// them as children of the turn; none when none are discovered.
func (c *Core) turnSkills(p *turnPrep, requester agent.PermissionRequester) ([]agent.SkillSpec, agent.SkillRunner) {
	discovered, err := skills.DiscoverCached(c.workspaceRoot)
	if err != nil || len(discovered) == 0 {
		return nil, nil
	}
	refs, _ := skills.DiscoverRefs(c.workspaceRoot)
	return skillrun.Specs(discovered), skillrun.New(skillrun.Config{
		Cfg:                 c.cfg,
		Skills:              discovered,
		Refs:                refs,
		Client:              p.custom.llmClient,
		FixedClient:         c.llmClientInjected,
		Validator:           c.validator,
		Runner:              c.tools,
		AgentLogger:         p.agentLogger,
		MaxSteps:            c.cfg.Agent.MaxSteps,
		AllowExec:           p.allowExec,
		AllowWeb:            p.allowWeb,
		AllowBrowser:        p.allowBrowser,
		PermissionRequester: requester,
		Events:              app.ChildEvents{Notify: p.childCfg.NotifyAgentEvent, Stream: p.childCfg.ChildEventSink},
	})
}

func skillsAllowedInMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case string(agent.ModeAsk), string(agent.ModePlan), string(agent.ModeExplore),
		string(agent.ModeVerifier),
		string(agent.ModeArchitecture), string(agent.ModeCompaction),
		string(agent.ModeTitle), string(agent.ModeSummary):
		return false
	default:
		return true
	}
}

func (c *Core) classifyAgentMode(ctx context.Context, query string, logger *llm.Logger) autorouter.Decision {
	fallback := autorouter.HeuristicClassify(query)
	if c.cfg == nil || !c.cfg.AutoRouter.ResolvedEnabled() {
		return fallback
	}
	client := c.autoRouterClient(logger)
	return autorouter.Classify(ctx, client, query)
}

func (c *Core) autoRouterClient(logger *llm.Logger) llm.Client {
	if c == nil || c.cfg == nil {
		return c.llmClient
	}
	if c.llmClientInjected {
		return c.llmClient
	}
	provider := strings.TrimSpace(c.cfg.AutoRouter.Provider)
	model := strings.TrimSpace(c.cfg.AutoRouter.Model)
	if provider == "" {
		provider = strings.TrimSpace(c.cfg.LLM.Router.FastProvider)
	}
	// Same fallback as compaction: use named providers.fast when present so
	// mode=agent classification does not burn the main (slow) model.
	if provider == "" && model == "" {
		if _, ok := c.cfg.FindProvider("fast"); ok {
			provider = "fast"
		}
	}
	if provider == "" && model == "" {
		return c.llmClient
	}
	client, _, _, err := c.resolveNamedClient(provider, model, logger)
	if err != nil || client == nil {
		return c.llmClient
	}
	return client
}

// compactionClient returns a cheap LLM for ModeCompaction / auto-summary.
// Prefer llm.router.fast_provider; else providers.fast when present.
// Nil → agent uses the main LLM client.
func (c *Core) compactionClient(logger *llm.Logger) llm.Client {
	client, _ := c.compactionClientWithContext(logger)
	return client
}

// compactionClientWithContext is compactionClient plus the resolved
// provider's own context window (num_ctx), so callers can size compaction
// requests against the ACTUAL model answering them (see
// agent.Options.CompactionContextTokens) instead of the main model's window.
func (c *Core) compactionClientWithContext(logger *llm.Logger) (llm.Client, int) {
	if c == nil || c.cfg == nil || c.llmClientInjected {
		return nil, 0
	}
	provider := strings.TrimSpace(c.cfg.LLM.Router.FastProvider)
	if provider == "" {
		if _, ok := c.cfg.FindProvider("fast"); ok {
			provider = "fast"
		}
	}
	if provider == "" {
		return nil, 0
	}
	client, _, _, err := c.resolveNamedClient(provider, "", logger)
	if err != nil || client == nil {
		return nil, 0
	}
	ctxTok := 0
	if pcfg, ok := c.cfg.FindProvider(provider); ok {
		ctxTok = llm.ContextTokensFromConfig(pcfg)
	}
	return client, ctxTok
}

func (c *Core) resolveOrchestraPlanner(logger *llm.Logger) (llm.Client, string, string, bool) {
	if c == nil || c.cfg == nil || c.llmClientInjected {
		return nil, "", "", false
	}
	p := strings.TrimSpace(c.cfg.Orchestra.Planner.Provider)
	m := strings.TrimSpace(c.cfg.Orchestra.Planner.Model)
	if p == "" && m == "" {
		// Fall back to the L5 role binding from orchestra_routing.yaml.
		if role, ok := c.cfg.Routing.ResolveRole("L5"); ok {
			p = strings.TrimSpace(role.Provider)
			m = strings.TrimSpace(role.Model)
		}
	}
	if p == "" && m == "" {
		return nil, "", "", false
	}
	client, pl, ml, err := c.resolveNamedClient(p, m, logger)
	if err != nil || client == nil {
		return nil, "", "", false
	}
	return client, pl, ml, true
}

// resolveNamedClient builds an LLM client from providers: map and/or model override.
func (c *Core) resolveNamedClient(provider, model string, logger *llm.Logger) (llm.Client, string, string, error) {
	if c == nil || c.cfg == nil {
		return nil, "", "", fmt.Errorf("nil core/config")
	}
	if c.llmClientInjected {
		return c.llmClient, providerLabelOf(c.cfg), c.cfg.LLM.Model, nil
	}
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if provider == "" && model == "" {
		return c.llmClient, providerLabelOf(c.cfg), c.cfg.LLM.Model, nil
	}
	// The default client is wrapped at construction (core.go). A client
	// resolved by name — compaction, auto-routing, every model switch from the
	// UI — carries the same standby and router, or choosing a model silently
	// drops what the config asked for.
	client, used, err := app.ClientFor(c.cfg, provider, model, logger)
	if err != nil {
		return nil, "", "", err
	}
	if provider == "" {
		provider = providerLabelOf(c.cfg)
	}
	return client, provider, used.Model, nil
}

// tierEscalationSettings converts config → tasks settings (spec §5.5).
func tierEscalationSettings(t config.TierEscalationConfig) tasks.TierEscalationSettings {
	return tasks.TierEscalationSettings{
		Enabled:                  t.ResolvedEnabled(),
		FailuresBeforeEscalation: t.ResolvedFailuresBeforeEscalation(),
		MaxEscalatedRetries:      t.ResolvedMaxEscalatedRetries(),
		EscalationTier:           t.ResolvedEscalationTier(),
	}
}

func (c *Core) buildChildAgentConfig(maxPromptBytes int, usage agent.UsageRecorder, allowExec bool, logger *llm.Logger) tasks.ChildAgentConfig {
	out := tasks.ChildAgentConfig{
		MaxPromptBytes: maxPromptBytes,
		UsageTracker:   usage,
		Caps: tools.Capabilities{
			Exec: allowExec,
		},
	}
	if c == nil || c.cfg == nil {
		return out
	}
	out.CompactThresholdPct = c.cfg.EffectiveCompactThresholdPct()
	// Children compact their own history too — give them the cheap model.
	if cc, ctxTok := c.compactionClientWithContext(logger); cc != nil {
		out.CompactionClient = cc
		out.CompactionContextTokens = ctxTok
	}
	out.ModelContextTokens = int(c.cfg.EffectiveNumCtx())
	out.CompletionMaxTokens = c.cfg.LLM.MaxTokens
	out.ToolDigestBytes = c.cfg.Agent.ResolvedToolDigestBytes()
	out.HistoryPruneKeepRecent = c.cfg.Agent.ResolvedHistoryPruneKeepRecent()
	out.ProviderLabel = providerLabelOf(c.cfg)
	out.ModelLabel = c.cfg.LLM.Model
	out.MaxWorkerRetries = c.cfg.Orchestra.ResolvedMaxWorkerRetries()
	enabled := c.cfg.Orchestra.ResolvedWorkerVerifyEnabled()
	out.WorkerVerifyEnabled = &enabled
	out.MaxWorkerVerifyRetries = c.cfg.Orchestra.ResolvedMaxWorkerVerifyRetries()
	llmVerify := c.cfg.Orchestra.ResolvedWorkerLLMVerifyEnabled()
	out.WorkerLLMVerifyEnabled = &llmVerify
	out.WorkerVerifyAffectedTests = c.cfg.Orchestra.WorkerVerifyAffectedTests
	out.WorkerVerifyFrontendTypecheck = c.cfg.Orchestra.WorkerVerifyFrontendTypecheck
	out.MaxClarificationRounds = c.cfg.Orchestra.ResolvedMaxClarificationRounds()
	out.RelayViaLLM = c.cfg.Orchestra.ResolvedRelayViaLLM()
	out.PhaseTimeouts = orchestrastate.PhaseTimeouts{
		DiscoveryS:       c.cfg.Orchestra.PhaseTimeouts.ResolvedDiscoveryS(),
		ContractS:        c.cfg.Orchestra.PhaseTimeouts.ResolvedContractS(),
		LeadBriefS:       c.cfg.Orchestra.PhaseTimeouts.ResolvedLeadBriefS(),
		BlockedEscalateS: c.cfg.Orchestra.PhaseTimeouts.ResolvedBlockedEscalateS(),
	}
	out.TierEscalation = tierEscalationSettings(c.cfg.Orchestra.TierEscalation)
	out.LLMStepTimeout = time.Duration(c.cfg.LLM.TimeoutS) * time.Second
	out.MaxStepsCap = c.cfg.Agent.ResolvedChildMaxSteps()
	out.AgentLogger = logger
	if c.cfg.Web.Confirm != nil && !*c.cfg.Web.Confirm {
		out.Caps.Web = true
	}
	out.ResolveClient = func(provider, model string) (llm.Client, string, string, error) {
		return c.resolveNamedClient(provider, model, logger)
	}
	out.ResolveTier = func(tier string) (provider, model string, ok bool) {
		return c.cfg.ResolveTierBinding(tier)
	}
	// The guards read the project as the agent behind ctx sees it: what the
	// Leads wrote is staged in this turn, not on disk yet (ORC-6).
	out.GuardSpawn = func(ctx context.Context, subagentType string) error {
		return orchestrastate.GuardSpawn(c.cfg.ProjectRoot, c.tools.View(ctx), c.cfg.Orchestra.ResolvedPhaseEnforcement(), subagentType)
	}
	out.GuardContractRefs = func(ctx context.Context, refs []contract.Ref) error {
		return orchestrastate.GuardWorkOrderContract(c.cfg.ProjectRoot, c.tools.View(ctx), c.cfg.Orchestra.ResolvedPhaseEnforcement(), refs)
	}
	out.RouteTaskType = func(taskType string) (tasks.TaskTypeRoute, bool) {
		rule, ok := c.cfg.Routing.Route(taskType)
		if !ok {
			return tasks.TaskTypeRoute{}, false
		}
		route := tasks.TaskTypeRoute{
			SubagentType: rule.SubagentType,
			Tier:         rule.Tier,
		}
		if role, found := c.cfg.Routing.ResolveRole(rule.RequiredTier); found {
			route.Provider = strings.TrimSpace(role.Provider)
			route.Model = strings.TrimSpace(role.Model)
		}
		return route, true
	}
	return out
}
