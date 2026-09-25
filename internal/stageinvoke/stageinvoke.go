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

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/app"
	"github.com/orchestra/orchestra/internal/config"
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

	// Events is where a stage's lifecycle and stream go.
	Events app.ChildEvents
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
	if (s.Provider != "" || s.Model != "") && c.Cfg != nil {
		// A fresh client with the standby and router its config asks for
		// (app.ClientFor), and the stage's logger set on it before anyone
		// else holds it.
		client, _, err := app.ClientFor(c.Cfg, s.Provider, s.Model, c.AgentLogger)
		if err != nil {
			return "", fmt.Errorf("skill %q: %w", skillName, err)
		}
		childClient = client
	}

	opts := buildAgentOptions(c, childTools, systemPrompt)

	// A stage is a child of the workflow's turn: it writes into a layer of
	// its own, committed when it succeeds and dropped when it fails, so a
	// failed attempt leaves nothing for the next one to trip over.
	out, err := app.RunChild(ctx, app.ChildRun{
		Client:    childClient,
		Validator: c.Validator,
		Tools:     c.Runner,
		Options:   opts,
		Goal:      userQuery,
		Kind:      "stage:" + skillName,
		Events:    c.Events,
	})
	if err != nil {
		return "", fmt.Errorf("skill %q: %w", skillName, err)
	}
	if out.Result != nil && out.Result.SubtaskResult != "" {
		return out.Result.SubtaskResult, nil
	}
	for i := len(out.History) - 1; i >= 0; i-- {
		if out.History[i].Role == llm.RoleAssistant && strings.TrimSpace(out.History[i].Content) != "" {
			return out.History[i].Content, nil
		}
	}
	patchCount := 0
	if out.Result != nil {
		patchCount = len(out.Result.Patches)
	}
	return fmt.Sprintf("skill %q finished (no text output; %d patch(es))", skillName, patchCount), nil
}

// buildAgentOptions assembles agent.Options from the shared config + per-skill
// overrides. Keeping it as a free function makes it trivial to unit-test the
// option mapping in isolation.
//
// Workflow stages are child agents — they end by calling task_result with the
// marker/output the runner expects — so they are built by app.ChildOptions,
// with the project's budgets, breakers and permission rules. Without a config
// they fall back to 24 steps and the agent's defaults.
func buildAgentOptions(c Config, childTools []llm.ToolDef, systemPrompt string) agent.Options {
	var settings *app.Settings
	if c.Cfg != nil {
		s := app.SettingsFrom(c.Cfg)
		settings = &s
	}
	return app.ChildOptions(settings, func(o *agent.Options) {
		if o.MaxSteps <= 0 {
			o.MaxSteps = 24
		}
		if settings != nil {
			// Stages run on the turn's model, so they keep its prompt family.
			o.PromptFamily = settings.PromptFamily
		}
		o.CompactionClient = c.CompactionClient
		o.CompactionContextTokens = c.CompactionContextTokens
		o.AllowExec = c.AllowExec
		o.AllowWeb = c.AllowWeb
		o.AllowBrowser = c.AllowBrowser
		o.CustomTools = childTools
		o.SystemPromptOverride = systemPrompt
		o.AgentLogger = c.AgentLogger
		o.HooksRunner = c.HooksRunner
		o.UsageTracker = c.UsageTracker
		o.ProviderLabel = c.ProviderLabel
		o.ModelLabel = c.ModelLabel
		o.PermissionRequester = c.PermissionRequester
		// SubtaskRunner / SkillRunner intentionally nil — the workflow runner is
		// the single source of orchestration; stages can't spawn their own.
	})
}
