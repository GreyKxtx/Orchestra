// Package stageinvoke supplies a single, shared workflow.StageInvoker that
// spawns a fresh child agent per stage invocation using a discovered skill's
// body as the system prompt and its tools list as the tool allow-list.
//
// Before this package existed, internal/cli/workflow.go and
// internal/core/workflow.go each carried a ~100-line near-duplicate of the
// same logic ("workflowStageInvoker" and "coreStageInvoker"). The two
// invokers drifted: the CLI variant wired the agent's full limit set
// (MaxInvalidRetries, MaxPromptBytes, CompactThresholdPct, LLMStepTimeout,
// PermissionRules, AgentLogger, HooksRunner, UsageTracker, ProviderLabel,
// ModelLabel) and the core variant carried only MaxSteps and the consent
// requester. Workflows launched over JSON-RPC therefore ran with worse
// safety/observability than the same workflow run from the CLI.
//
// Invoker is the single implementation; both call sites construct it the
// same way. Caller fills the optional fields it cares about (the CLI passes
// loggers/usage tracker; core passes the PermissionRequester). The agent
// limits are always sourced from the project config when present.
package stageinvoke

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/config"
	promptpkg "github.com/orchestra/orchestra/internal/prompt"
	"github.com/orchestra/orchestra/internal/skills"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// Config is the construction parameters for Invoker.
//
// Required: Cfg, Skills, Client, Validator, Runner.
// Everything else is optional; sensible defaults apply.
type Config struct {
	Cfg       *config.ProjectConfig
	Skills    []*skills.Skill
	Refs      map[string]string
	Client    llm.Client
	Validator *schema.Validator
	Runner    *tools.Runner

	AllowExec    bool
	AllowWeb     bool
	AllowBrowser bool

	// Optional integrations.
	AgentLogger         *llm.Logger
	HooksRunner         agent.HooksRunner
	UsageTracker        agent.UsageRecorder
	ProviderLabel       string
	ModelLabel          string
	PermissionRequester agent.PermissionRequester

	// CompactionClient is the cheap model used for stage history compaction
	// (llm.router.fast_provider / providers.fast). Nil = the stage compacts
	// with its own model.
	CompactionClient        llm.Client
	CompactionContextTokens int
}

// Invoker implements workflow.StageInvoker.
type Invoker struct {
	cfg Config
}

// New returns a new Invoker. Cfg.Cfg must be non-nil — every field below
// reads from it.
func New(cfg Config) *Invoker { return &Invoker{cfg: cfg} }

// Invoke runs the named skill as a single agent turn and returns the text
// the workflow runner should treat as the stage's output. The preference
// order is:
//  1. SubtaskResult (set when the skill ends by calling task_result),
//  2. the last non-empty assistant message (completion markers live here),
//  3. a synthetic patch-count summary as a last resort.
func (inv *Invoker) Invoke(ctx context.Context, skillName, userQuery string) (string, error) {
	c := inv.cfg
	s := skills.Find(c.Skills, skillName)
	if s == nil {
		return "", fmt.Errorf("unknown skill %q", skillName)
	}
	for _, t := range s.Tools {
		if !config.ValidAgentTool(t) {
			return "", fmt.Errorf("skill %q: invalid tool name %q", skillName, t)
		}
	}

	systemPrompt, err := skills.PrepareBody(s.Body, userQuery, c.Refs)
	if err != nil {
		return "", fmt.Errorf("skill %q: %w", skillName, err)
	}

	var childTools []llm.ToolDef
	if len(s.Tools) > 0 {
		resolved, err := tools.ResolveToolNamesWithPolicy(s.Tools, tools.Capabilities{
			Exec:    c.AllowExec,
			Web:     c.AllowWeb,
			Browser: c.AllowBrowser,
		})
		if err != nil {
			return "", fmt.Errorf("skill %q: resolve tools: %w", skillName, err)
		}
		// Defensive: when policy filtering removes every tool a skill requested
		// (e.g. a skill whose only tools are bash + git.commit run without
		// --allow-exec), an empty tool slice leaves the child agent with no
		// way to act. It will burn through MaxFinalFailures retries and exit
		// with an opaque error. Fall back to the default child tool-set with
		// an unmistakable error so the operator sees the real cause.
		if len(resolved) == 0 {
			return "", fmt.Errorf("skill %q: every tool in `tools: %v` is disabled by current policy "+
				"(allow_exec=%v allow_web=%v allow_browser=%v) — re-run with the appropriate --allow-* flag or drop the gated tools from the skill",
				skillName, s.Tools, c.AllowExec, c.AllowWeb, c.AllowBrowser)
		}
		childTools = resolved
	} else {
		childTools = tools.ListToolsForChild()
	}

	// childClient: by default we inherit the shared client (whose logger was
	// configured once at construction time). Only when the skill requests a
	// provider or model override do we build a NEW client — and only on that
	// fresh client may we call SetLogger, because the shared client is used
	// concurrently by parallel cohort stages and SetLogger is a plain pointer
	// write (data race under the race detector if called from goroutines).
	childClient := c.Client
	overridden := false
	if s.Provider != "" && c.Cfg != nil {
		provCfg, ok := c.Cfg.FindProvider(s.Provider)
		if !ok {
			return "", fmt.Errorf("skill %q: provider %q not found", skillName, s.Provider)
		}
		if s.Model != "" {
			provCfg.Model = s.Model
		}
		childClient = llm.NewClient(provCfg)
		overridden = true
	} else if s.Model != "" && c.Cfg != nil {
		over := c.Cfg.LLM
		over.Model = s.Model
		childClient = llm.NewClient(over)
		overridden = true
	}
	if overridden {
		if oc, ok := llm.AsOpenAIClient(childClient); ok && c.AgentLogger != nil {
			oc.SetLogger(c.AgentLogger)
		}
		childClient = llm.MaybeWrapFallback(childClient, c.Cfg.LLMRegistry(), c.Cfg.LLM, c.AgentLogger)
		// Preserve router-fallback semantics: the base shared Client passed in
		// was already wrapped via MaybeWrapRouter by the caller (cli/workflow
		// run-up). A fresh per-skill client built from provider/model overrides
		// would otherwise lose that wrap, silently disabling the fast-path /
		// fallback routing for that stage. Re-wrap so behaviour is consistent.
		childClient = llm.MaybeWrapRouter(childClient, c.Cfg.LLMRegistry(), c.Cfg.LLM.Router)
	}

	opts := buildAgentOptions(c, childTools, systemPrompt)

	ag, err := agent.New(childClient, c.Validator, c.Runner, opts)
	if err != nil {
		return "", fmt.Errorf("skill %q: %w", skillName, err)
	}

	history, res, runErr := ag.Run(ctx, nil, userQuery)
	if runErr != nil {
		return "", fmt.Errorf("skill %q: %w", skillName, runErr)
	}
	if res != nil && res.SubtaskResult != "" {
		return res.SubtaskResult, nil
	}
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == llm.RoleAssistant && strings.TrimSpace(history[i].Content) != "" {
			return history[i].Content, nil
		}
	}
	patchCount := 0
	if res != nil {
		patchCount = len(res.Patches)
	}
	return fmt.Sprintf("skill %q finished (no text output; %d patch(es))", skillName, patchCount), nil
}

// buildAgentOptions assembles agent.Options from the shared config + per-skill
// overrides. Keeping it as a free function makes it trivial to unit-test the
// option mapping in isolation.
func buildAgentOptions(c Config, childTools []llm.ToolDef, systemPrompt string) agent.Options {
	maxSteps := 24
	var (
		maxInvalid     int
		maxDenied      int
		maxToolErrors  int
		maxFinalFails  int
		maxPromptBytes int
		compactPct     int
		modelCtx       int
		completionMax  int
		stepTimeout    time.Duration
		promptFamily   string
	)
	var permRules []config.PermissionRule
	if c.Cfg != nil {
		if c.Cfg.Agent.MaxSteps > 0 {
			maxSteps = c.Cfg.Agent.MaxSteps
		}
		maxInvalid = c.Cfg.Agent.MaxInvalidRetries
		maxDenied = c.Cfg.Agent.MaxDeniedRepeats
		maxToolErrors = c.Cfg.Agent.MaxToolErrors
		maxFinalFails = c.Cfg.Agent.MaxFinalFailures
		maxPromptBytes = c.Cfg.EffectiveMaxPromptBytes()
		compactPct = c.Cfg.EffectiveCompactThresholdPct()
		modelCtx = int(c.Cfg.EffectiveNumCtx())
		completionMax = c.Cfg.LLM.MaxTokens
		stepTimeout = time.Duration(c.Cfg.LLM.TimeoutS) * time.Second
		permRules = c.Cfg.Permissions.Rules
		promptFamily = promptpkg.ResolvePromptFamily(c.Cfg.LLM.PromptFamily, c.Cfg.LLM.Model)
	}

	// Workflow stages are child agents — they end by calling task_result with
	// the marker/output the runner expects. Without IsChild=true, the
	// main-agent guard rejects task_result as invalid.
	return agent.Options{
		IsChild:              true,
		MaxSteps:             maxSteps,
		MaxInvalidRetries:    maxInvalid,
		MaxDeniedToolRepeats: maxDenied,
		MaxToolErrorRepeats:  maxToolErrors,
		MaxFinalFailures:     maxFinalFails,
		MaxPromptBytes:       maxPromptBytes,
		CompactThresholdPct:  compactPct,
		PromptFamily:         promptFamily,

		CompactionClient:        c.CompactionClient,
		CompactionContextTokens: c.CompactionContextTokens,
		ModelContextTokens:      modelCtx,
		CompletionMaxTokens:     completionMax,
		LLMStepTimeout:          stepTimeout,
		AllowExec:               c.AllowExec,
		AllowWeb:                c.AllowWeb,
		AllowBrowser:            c.AllowBrowser,
		CustomTools:             childTools,
		SystemPromptOverride:    systemPrompt,
		PermissionRules:         permRules,
		AgentLogger:             c.AgentLogger,
		HooksRunner:             c.HooksRunner,
		UsageTracker:            c.UsageTracker,
		ProviderLabel:           c.ProviderLabel,
		ModelLabel:              c.ModelLabel,
		PermissionRequester:     c.PermissionRequester,
		// SubtaskRunner / SkillRunner intentionally nil — the workflow runner is
		// the single source of orchestration; stages can't spawn their own.
	}
}
