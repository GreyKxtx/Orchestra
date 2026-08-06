package core

import (
	"fmt"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/hooks"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/protocol"
	promptpkg "github.com/orchestra/orchestra/internal/prompt"
	"github.com/orchestra/orchestra/internal/schema"
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
}

type agentLaunch struct {
	Opts       agent.Options
	Custom     customAgentOpts
	Usage      *usage.Tracker
	TaskRunner *tasks.TaskRunner
	Profile    string
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

	profileName, err := resolveProfileName(c.cfg, spec.Profile)
	if err != nil {
		return nil, err
	}

	var respFmt *llm.ResponseFormat
	if c.cfg.LLM.ResponseFormatType != "" {
		respFmt = &llm.ResponseFormat{Type: c.cfg.LLM.ResponseFormatType}
		if c.cfg.LLM.ResponseFormatType == "json_schema" {
			respFmt.Schema = schema.AgentStepSchemaRaw()
			respFmt.SchemaName = "agent_step"
		}
	}

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

	var onEvent func(agent.AgentEvent)
	if spec.OnEvent != nil {
		env := spec.EventEnvelope
		if env.TurnID == "" {
			env.TurnID = NewTurnID()
		}
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

	customOpts, err := c.resolveCustomAgentOpts(spec.Mode, agentLogger)
	if err != nil {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, err.Error(), nil)
	}

	usageLabel := spec.UsageLabel
	if usageLabel == "" {
		usageLabel = "agent.run"
	}
	usageTracker := newAgentUsageTracker(c.cfg, usageLabel)
	taskRunner := tasks.New(c.llmClient, c.validator, c.tools, childAgentConfig(c.cfg, maxPromptBytes, usageTracker))

	planPath := strings.TrimSpace(spec.PlanPath)
	if planPath == "" {
		planPath = resolvePlanPath(spec.Mode, "", "")
	}

	opts := agent.Options{
		MaxSteps:             maxSteps,
		MaxInvalidRetries:    maxRetries,
		MaxDeniedToolRepeats: c.cfg.Agent.MaxDeniedRepeats,
		MaxToolErrorRepeats:  c.cfg.Agent.MaxToolErrors,
		MaxFinalFailures:     c.cfg.Agent.MaxFinalFailures,
		MaxPromptBytes:       maxPromptBytes,
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
		Mode:                 agent.Mode(spec.Mode),
		SystemPromptOverride: customOpts.systemPromptOverride,
		CustomTools:          customOpts.customTools,
		OnEvent:              onEvent,
		AgentLogger:          agentLogger,
		SubtaskRunner:        taskRunner,
		HooksRunner:          hooksRunner,
		ExtraTools:           c.mcpToolDefs(),
		PermissionRequester:  convertPermissionRequester(spec.PermissionRequester),
		QuestionAsker:        spec.QuestionAsker,
		UsageTracker:         usageTracker,
		ProviderLabel:        providerLabelOf(c.cfg),
		ModelLabel:           c.cfg.LLM.Model,
		PlanPath:             planPath,
		SessionID:            spec.SessionID,
		AutoSessionMemory:    spec.AutoSessionMemory,
	}
	agent.ApplyHistoryConfig(&opts, c.cfg)

	if err := agent.ApplyProfile(&opts, profileName, false); err != nil {
		return nil, protocol.NewError(protocol.InvalidParams, err.Error(), nil)
	}
	if customOpts.systemPromptOverride != "" {
		opts.SystemPromptOverride = customOpts.systemPromptOverride
	}
	if customOpts.customTools != nil {
		opts.CustomTools = customOpts.customTools
	}

	return &agentLaunch{
		Opts:       opts,
		Custom:     customOpts,
		Usage:      usageTracker,
		TaskRunner: taskRunner,
		Profile:    profileName,
	}, nil
}
