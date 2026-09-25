// Package app is the composition root for agents: the one place
// agent.Options are made.
//
// Options used to be assembled by hand in eleven places — the CLI, the core's
// launch, core/skill.go, skillrun, stageinvoke, the task runner and its LLM
// verifier, and three pipeline stages — each reading .orchestra.yml its own
// way (ARCH-1, ARCH-10). They drifted: the CLI lost exec.allow and the worker
// LLM verifier, skill children ran with the agent's 25-second step timeout
// and no prompt budget, and task children ignored permission rules.
//
// Settings resolves what an agent takes from config once. TurnOptions builds a
// top-level turn and ChildOptions a subagent; both take the caller's per-run
// wiring as a function, so the defaults, and the order in which profiles and
// overrides are applied after it, live here and nowhere else.
package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/config"
	promptpkg "github.com/orchestra/orchestra/internal/prompt"
	"github.com/orchestra/orchestra/llm"
)

// Settings is what an agent run takes from .orchestra.yml, resolved once.
type Settings struct {
	MaxSteps          int
	MaxInvalidRetries int
	MaxDeniedRepeats  int
	MaxToolErrors     int
	MaxFinalFailures  int

	MaxPromptBytes       int
	CompactThresholdPct  int
	ModelContextTokens   int
	CompletionMaxTokens  int
	BytesPerContextToken int
	LLMStepTimeout       time.Duration
	ChildTimeoutMS       int

	PromptFamily    string
	PermissionRules []config.PermissionRule
	ExecAllow       []string
	ExecDeny        []string

	HumanGates       map[string]bool
	StateMaxBytes    int
	PhaseEnforcement string

	cfg *config.ProjectConfig
}

// SettingsFrom resolves cfg. A nil config gives zero Settings, which leave
// every field to the agent's own defaults.
func SettingsFrom(cfg *config.ProjectConfig) Settings {
	if cfg == nil {
		return Settings{}
	}
	return Settings{
		MaxSteps:             cfg.Agent.MaxSteps,
		MaxInvalidRetries:    cfg.Agent.MaxInvalidRetries,
		MaxDeniedRepeats:     cfg.Agent.MaxDeniedRepeats,
		MaxToolErrors:        cfg.Agent.MaxToolErrors,
		MaxFinalFailures:     cfg.Agent.MaxFinalFailures,
		MaxPromptBytes:       cfg.EffectiveMaxPromptBytes(),
		CompactThresholdPct:  cfg.EffectiveCompactThresholdPct(),
		ModelContextTokens:   int(cfg.EffectiveNumCtx()),
		CompletionMaxTokens:  cfg.LLM.MaxTokens,
		BytesPerContextToken: cfg.Agent.ResolvedBytesPerContextToken(),
		LLMStepTimeout:       time.Duration(cfg.LLM.TimeoutS) * time.Second,
		ChildTimeoutMS:       cfg.Agent.ResolvedChildTimeoutMS(),
		PromptFamily:         promptpkg.ResolvePromptFamily(cfg.LLM.PromptFamily, cfg.LLM.Model),
		PermissionRules:      cfg.Permissions.Rules,
		ExecAllow:            cfg.Exec.Allow,
		ExecDeny:             cfg.Exec.Deny,
		HumanGates:           cfg.Orchestra.RequiredGates(),
		StateMaxBytes:        cfg.Orchestra.ResolvedStateMaxBytes(),
		PhaseEnforcement:     cfg.Orchestra.ResolvedPhaseEnforcement(),
		cfg:                  cfg,
	}
}

// Config is the project config the settings came from, or nil.
func (s Settings) Config() *config.ProjectConfig { return s.cfg }

// limits are the loop's budgets and breakers, which every agent takes.
func (s Settings) limits() agent.Options {
	return agent.Options{
		MaxSteps:             s.MaxSteps,
		MaxInvalidRetries:    s.MaxInvalidRetries,
		MaxDeniedToolRepeats: s.MaxDeniedRepeats,
		MaxToolErrorRepeats:  s.MaxToolErrors,
		MaxFinalFailures:     s.MaxFinalFailures,
		MaxPromptBytes:       s.MaxPromptBytes,
		CompactThresholdPct:  s.CompactThresholdPct,
		ModelContextTokens:   s.ModelContextTokens,
		CompletionMaxTokens:  s.CompletionMaxTokens,
		BytesPerContextToken: s.BytesPerContextToken,
		LLMStepTimeout:       s.LLMStepTimeout,
		PromptFamily:         s.PromptFamily,
		PermissionRules:      s.PermissionRules,
	}
}

// TurnOptions builds a top-level turn: the settings, then what set wires in
// for this turn (mode, consent, runners, callbacks), then the history config,
// the profile and the rules that must win over a profile.
func TurnOptions(s Settings, profile string, set func(*agent.Options)) (agent.Options, error) {
	o := s.limits()
	o.ExecAllow = s.ExecAllow
	o.ExecDeny = s.ExecDeny
	o.HumanGates = s.HumanGates
	o.StateMaxBytes = s.StateMaxBytes
	o.PhaseEnforcement = s.PhaseEnforcement
	o.ChildTimeoutMS = s.ChildTimeoutMS
	if set != nil {
		set(&o)
	}
	agent.ApplyHistoryConfig(&o, s.cfg)

	// A named agent's prompt and tools are its definition; a profile must not
	// filter them away.
	prompt, tools := o.SystemPromptOverride, o.CustomTools
	// preserveNonZero: agent.max_steps from .orchestra.yml wins over the
	// profile presets (fast=10, precision=36); otherwise a profile silently
	// undoes max_steps: 200 and the turn "falls" after 10–36 steps.
	if err := agent.ApplyProfile(&o, profile, true); err != nil {
		return agent.Options{}, err
	}
	agent.FillRetryLimits(&o, o.ProviderLabel)
	// llm.timeout_s always wins over any profile or default residue.
	if s.LLMStepTimeout > 0 {
		o.LLMStepTimeout = s.LLMStepTimeout
	}
	if prompt != "" {
		o.SystemPromptOverride = prompt
	}
	if tools != nil {
		o.CustomTools = tools
	}
	return o, nil
}

// ChildOptions builds a subagent: a child of the turn (it answers through
// task_result), with the budgets, breakers and permission rules of s, then what
// set wires in (its mode, tools, model and consent). A child runs on its own
// model, so the prompt family follows that model unless set chooses one.
//
// s may be nil for a child with nothing from config; the agent's own defaults
// then apply.
func ChildOptions(s *Settings, set func(*agent.Options)) agent.Options {
	var o agent.Options
	if s != nil {
		o = s.limits()
		o.PromptFamily = ""
	}
	o.IsChild = true
	if set != nil {
		set(&o)
	}
	if o.PromptFamily == "" {
		o.PromptFamily = promptpkg.ResolvePromptFamily("", o.ModelLabel)
	}
	return o
}

// ClientFor builds the client for a provider and/or model named in config —
// an agents: entry, a skill, a tier, the compaction or routing model. An empty
// provider means the main endpoint; an empty model, the provider's own.
//
// Every such client carries the standby and router its config asks for
// (llm.BuildClient). Five call sites used to build them with llm.NewClient,
// each wrapping a different subset, so choosing a model for a skill or a
// custom agent silently dropped the fallback the project configured.
func ClientFor(cfg *config.ProjectConfig, provider, model string, logger *llm.Logger) (llm.Client, llm.LLMConfig, error) {
	if cfg == nil {
		return nil, llm.LLMConfig{}, fmt.Errorf("no project config")
	}
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	llmCfg := cfg.LLM
	if provider != "" {
		p, ok := cfg.FindProvider(provider)
		if !ok {
			return nil, llm.LLMConfig{}, fmt.Errorf("provider %q not found in providers: section", provider)
		}
		// Fail fast with an actionable message instead of letting the request
		// bounce off the gateway as an opaque 401 ("User not found").
		if cat, catOK := llm.FindCatalogProvider(provider); catOK && cat.NeedsKey && strings.TrimSpace(p.APIKey) == "" {
			return nil, llm.LLMConfig{}, fmt.Errorf(
				"provider %q has no api_key configured — add it in Settings → Providers (or providers.%s.api_key in .orchestra.yml)",
				provider, provider)
		}
		llmCfg = p
	}
	if model != "" {
		llmCfg.Model = model
	}
	return llm.BuildClient(llmCfg, cfg.LLMRegistry(), logger), llmCfg, nil
}
