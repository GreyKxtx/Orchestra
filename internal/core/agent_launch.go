package core

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/autorouter"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/hooks"
	"github.com/orchestra/orchestra/llm"
	promptpkg "github.com/orchestra/orchestra/internal/prompt"
	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/internal/skillrun"
	"github.com/orchestra/orchestra/internal/skills"
	"github.com/orchestra/orchestra/internal/tasks"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/internal/usage"
)

// agentLaunchSpec is the shared input for building agent.Options from Core config.
// AgentRun and SessionMessage both go through prepareAgentLaunch so new knobs
// (digest, prune, memory, …) cannot drift between entry points.
type agentLaunchSpec struct {
	Mode      string
	Profile   string
	PlanPath  string
	SessionID string
	Query     string // user turn text; used by mode=agent auto-router

	Apply     bool
	Backup    bool
	AllowExec bool
	Debug     bool

	MaxSteps          int
	MaxInvalidRetries int
	MaxPromptBytes    int

	InitialTodos []tools.TodoItem

	AutoSessionMemory bool // false for one-shot agent.run; config-resolved for sessions
	UsageLabel        string

	OnEvent             func(method string, params any)
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

func (c *Core) prepareAgentLaunch(spec agentLaunchSpec) (*agentLaunch, error) {
	if c == nil || c.cfg == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core config is nil", nil)
	}

	// Wire TUI/CLI consent into LSP auto-provision for this turn.
	// Warmup is best-effort and must not block the agent loop on npm/go install.
	if c.tools != nil {
		c.tools.SetLSPInstallConsent(spec.PermissionRequester)
		go c.tools.WarmupLSP(context.Background())
	}

	profileName, err := resolveProfileName(c.cfg, spec.Profile)
	if err != nil {
		return nil, err
	}

	var respFmt *llm.ResponseFormat
	respFmt = agent.ResolveResponseFormat(c.cfg.LLM, providerLabelOf(c.cfg))

	maxSteps := spec.MaxSteps
	if maxSteps <= 0 {
		maxSteps = c.cfg.Agent.MaxSteps
	}
	maxRetries := spec.MaxInvalidRetries
	if maxRetries <= 0 {
		maxRetries = c.cfg.Agent.MaxInvalidRetries
	}
	maxPromptBytes := spec.MaxPromptBytes
	if maxPromptBytes <= 0 {
		maxPromptBytes = c.cfg.EffectiveMaxPromptBytes()
	}

	promptFamily := promptpkg.ResolvePromptFamily(c.cfg.LLM.PromptFamily, c.cfg.LLM.Model)

	env := spec.EventEnvelope
	if env.TurnID == "" {
		env.TurnID = NewTurnID()
	}
	var onEvent func(agent.AgentEvent)
	if spec.OnEvent != nil {
		onEvent = buildAgentOnEvent(spec.OnEvent, env)
	}

	allowExec := spec.AllowExec
	if c.cfg.Exec.Confirm != nil && !*c.cfg.Exec.Confirm {
		allowExec = true
	}

	var agentLogger *llm.Logger
	if c.llmClient != nil {
		if oc, ok := c.llmClient.(*llm.OpenAIClient); ok {
			agentLogger = oc.GetLogger()
		}
	}

	var hooksRunner agent.HooksRunner
	if hr := hooks.New(c.cfg.Hooks, c.workspaceRoot); hr != nil {
		hooksRunner = hr
	}

	requestedMode := strings.TrimSpace(spec.Mode)
	effectiveMode := requestedMode
	routeReason := ""
	routeConfidence := 0.0

	if strings.EqualFold(requestedMode, string(agent.ModeAgent)) {
		dec := c.classifyAgentMode(context.Background(), spec.Query, agentLogger)
		effectiveMode = dec.Mode
		routeReason = dec.Reason
		routeConfidence = dec.Confidence
		if spec.OnEvent != nil {
			spec.OnEvent("agent/event", mergeEventEnvelope(map[string]any{
				"step": 0,
				"type": "mode_route",
				"data": map[string]any{
					"from":       string(agent.ModeAgent),
					"to":         effectiveMode,
					"reason":     routeReason,
					"confidence": routeConfidence,
				},
			}, env))
		}
	}

	customOpts, err := c.resolveCustomAgentOpts(effectiveMode, agentLogger)
	if err != nil {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, err.Error(), nil)
	}

	// Orchestra Lead uses orchestra.planner provider/model when configured.
	if strings.EqualFold(effectiveMode, string(agent.ModeOrchestra)) {
		if client, pl, ml, ok := c.resolveOrchestraPlanner(agentLogger); ok {
			customOpts.llmClient = client
			if pl != "" {
				// labels applied below via opts
				_ = ml
			}
		}
	}

	usageLabel := spec.UsageLabel
	if usageLabel == "" {
		usageLabel = "agent.run"
	}
	usageTracker := newAgentUsageTracker(c.cfg, usageLabel)
	childCfg := c.buildChildAgentConfig(maxPromptBytes, usageTracker, allowExec, agentLogger)
	if spec.OnEvent != nil {
		childCfg.NotifyAgentEvent = func(params map[string]any) {
			if _, ok := params["task_id"]; ok {
				params["scope"] = "child"
			}
			spec.OnEvent("agent/event", mergeEventEnvelope(params, env))
		}
		childCfg.ChildEventSink = func(taskID, parentToolCallID, subagentType string) func(agent.AgentEvent) {
			meta := &ChildScopeMeta{
				TaskID:           taskID,
				ParentToolCallID: parentToolCallID,
				SubagentType:     subagentType,
			}
			return buildAgentOnEventWithChild(spec.OnEvent, env, meta)
		}
	}
	taskRunner := tasks.New(customOpts.llmClient, c.validator, c.tools, childCfg)

	planPath := strings.TrimSpace(spec.PlanPath)
	if planPath == "" {
		planPath = resolvePlanPath(effectiveMode, "", "")
	}

	providerLabel := providerLabelOf(c.cfg)
	modelLabel := c.cfg.LLM.Model
	if strings.EqualFold(effectiveMode, string(agent.ModeOrchestra)) {
		if p := strings.TrimSpace(c.cfg.Orchestra.Planner.Provider); p != "" {
			providerLabel = p
		}
		if m := strings.TrimSpace(c.cfg.Orchestra.Planner.Model); m != "" {
			modelLabel = m
		}
	}

	opts := agent.Options{
		MaxSteps:             maxSteps,
		MaxInvalidRetries:    maxRetries,
		MaxDeniedToolRepeats: c.cfg.Agent.MaxDeniedRepeats,
		MaxToolErrorRepeats:  c.cfg.Agent.MaxToolErrors,
		MaxFinalFailures:     c.cfg.Agent.MaxFinalFailures,
		MaxPromptBytes:       maxPromptBytes,
		ModelContextTokens:   int(c.cfg.EffectiveNumCtx()),
		CompletionMaxTokens:  c.cfg.LLM.MaxTokens,
		BytesPerContextToken: c.cfg.Agent.ResolvedBytesPerContextToken(),
		LLMStepTimeout:       time.Duration(c.cfg.LLM.TimeoutS) * time.Second,
		Apply:                spec.Apply,
		Backup:               spec.Backup,
		AllowExec:            allowExec,
		ExecAllow:            c.cfg.Exec.Allow,
		ExecDeny:             c.cfg.Exec.Deny,
		PermissionRules:      c.cfg.Permissions.Rules,
		InitialTodos:         spec.InitialTodos,
		Debug:                spec.Debug,
		ResponseFormat:       respFmt,
		PromptFamily:         promptFamily,
		Mode:                 agent.Mode(effectiveMode),
		SystemPromptOverride: customOpts.systemPromptOverride,
		CustomTools:          customOpts.customTools,
		OnEvent:              onEvent,
		AgentLogger:          agentLogger,
		SubtaskRunner:        taskRunner,
		HooksRunner:          hooksRunner,
		ExtraTools:           c.extraToolDefs(),
		PermissionRequester:  convertPermissionRequester(spec.PermissionRequester),
		QuestionAsker:        spec.QuestionAsker,
		UsageTracker:         usageTracker,
		ProviderLabel:        providerLabel,
		ModelLabel:           modelLabel,
		PlanPath:             planPath,
		SessionID:            spec.SessionID,
		AutoSessionMemory:    spec.AutoSessionMemory,
	}
	// Skills in TUI/core (parity with `orchestra apply`). Skip for read-only /
	// plan-only modes so skill_invoke cannot bypass write guards via a child.
	if skillsAllowedInMode(effectiveMode) {
		if discovered, err := skills.DiscoverCached(c.workspaceRoot); err == nil && len(discovered) > 0 {
			refs, _ := skills.DiscoverRefs(c.workspaceRoot)
			allowWeb := c.cfg.Web.Confirm != nil && !*c.cfg.Web.Confirm
			opts.Skills = skillrun.Specs(discovered)
			opts.SkillRunner = skillrun.New(
				c.cfg, discovered, refs, customOpts.llmClient, c.validator, c.tools, agentLogger,
				c.cfg.Agent.MaxSteps, allowExec, allowWeb, false,
			)
		}
	}
	agent.ApplyHistoryConfig(&opts, c.cfg)

	if cc, ctxTok := c.compactionClientWithContext(agentLogger); cc != nil {
		opts.CompactionClient = cc
		opts.CompactionContextTokens = ctxTok
	}

	// preserveNonZero=true: agent.max_steps from .orchestra.yml wins over
	// profile presets (fast=10, precision=36). Otherwise a selected profile silently
	// undoes max_steps: 200 and the turn "falls" after 10–36 steps.
	if err := agent.ApplyProfile(&opts, profileName, true); err != nil {
		return nil, protocol.NewError(protocol.InvalidParams, err.Error(), nil)
	}
	agent.FillRetryLimits(&opts, providerLabel)
	// llm.timeout_s always wins over any profile/default residue.
	if t := time.Duration(c.cfg.LLM.TimeoutS) * time.Second; t > 0 {
		opts.LLMStepTimeout = t
	}
	if customOpts.systemPromptOverride != "" {
		opts.SystemPromptOverride = customOpts.systemPromptOverride
	}
	if customOpts.customTools != nil {
		opts.CustomTools = customOpts.customTools
	}
	if len(spec.UserImages) > 0 {
		opts.UserImages = spec.UserImages
	}
	if spec.Multimodal || (c.cfg.LLM.Multimodal && len(spec.UserImages) > 0) {
		opts.MultimodalLLM = c.cfg.LLM.Multimodal
	}

	return &agentLaunch{
		Opts:            opts,
		Custom:          customOpts,
		Usage:           usageTracker,
		TaskRunner:      taskRunner,
		Profile:         profileName,
		RequestedMode:   requestedMode,
		EffectiveMode:   effectiveMode,
		RouteReason:     routeReason,
		RouteConfidence: routeConfidence,
		EventEnvelope:   env,
	}, nil
}

func skillsAllowedInMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case string(agent.ModeAsk), string(agent.ModePlan), string(agent.ModeExplore),
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
	if provider != "" {
		provCfg, ok := c.cfg.FindProvider(provider)
		if !ok {
			return nil, "", "", fmt.Errorf("provider %q not found in providers", provider)
		}
		if model != "" {
			provCfg.Model = model
		}
		client := llm.NewClient(provCfg)
		if oc, ok2 := client.(*llm.OpenAIClient); ok2 && logger != nil {
			oc.SetLogger(logger)
		}
		return client, provider, provCfg.Model, nil
	}
	if model != "" {
		overrideCfg := c.cfg.LLM
		overrideCfg.Model = model
		client := llm.NewClient(overrideCfg)
		if oc, ok := client.(*llm.OpenAIClient); ok && logger != nil {
			oc.SetLogger(logger)
		}
		return client, providerLabelOf(c.cfg), model, nil
	}
	return c.llmClient, providerLabelOf(c.cfg), c.cfg.LLM.Model, nil
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
	out.CompactThresholdPct = c.cfg.Agent.CompactThresholdPct
	out.ModelContextTokens = int(c.cfg.EffectiveNumCtx())
	out.CompletionMaxTokens = c.cfg.LLM.MaxTokens
	out.ToolDigestBytes = c.cfg.Agent.ResolvedToolDigestBytes()
	out.HistoryPruneKeepRecent = c.cfg.Agent.ResolvedHistoryPruneKeepRecent()
	out.ProviderLabel = providerLabelOf(c.cfg)
	out.ModelLabel = c.cfg.LLM.Model
	out.MaxWorkerRetries = c.cfg.Orchestra.ResolvedMaxWorkerRetries()
	out.LLMStepTimeout = time.Duration(c.cfg.LLM.TimeoutS) * time.Second
	out.MaxStepsCap = c.cfg.Agent.ResolvedChildMaxSteps()
	if c.cfg.Web.Confirm != nil && !*c.cfg.Web.Confirm {
		out.Caps.Web = true
	}
	out.ResolveClient = func(provider, model string) (llm.Client, string, string, error) {
		return c.resolveNamedClient(provider, model, logger)
	}
	out.ResolveTier = func(tier string) (provider, model string, ok bool) {
		t := c.cfg.Orchestra.FindTier(tier)
		if t == nil {
			return "", "", false
		}
		return t.Provider, t.Model, true
	}
	return out
}
