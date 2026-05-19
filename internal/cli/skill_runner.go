package cli

import (
	"context"
	"fmt"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/schema"
	"github.com/orchestra/orchestra/internal/skills"
	"github.com/orchestra/orchestra/internal/tools"
)

// cliSkillRunner implements agent.SkillRunner by spawning a fresh child
// agent that uses the named skill's body as the system prompt, its tool
// list as the tool filter, and its model/provider overrides for the LLM
// client. Runs synchronously; the returned string is either the child's
// final SubtaskResult or a concise patch-count summary if it produced
// patches instead of explicit results.
type cliSkillRunner struct {
	cfg          *config.ProjectConfig
	skills       []*skills.Skill
	refs         map[string]string
	baseClient   llm.Client
	validator    *schema.Validator
	toolRunner   *tools.Runner
	agentLogger  *llm.Logger
	maxSteps     int
	allowExec    bool
	allowWeb     bool
	allowBrowser bool
}

func newCLISkillRunner(
	cfg *config.ProjectConfig,
	discovered []*skills.Skill,
	refs map[string]string,
	baseClient llm.Client,
	validator *schema.Validator,
	toolRunner *tools.Runner,
	agentLogger *llm.Logger,
	maxSteps int,
	allowExec, allowWeb, allowBrowser bool,
) *cliSkillRunner {
	if maxSteps <= 0 {
		maxSteps = 12
	}
	return &cliSkillRunner{
		cfg:          cfg,
		skills:       discovered,
		refs:         refs,
		baseClient:   baseClient,
		validator:    validator,
		toolRunner:   toolRunner,
		agentLogger:  agentLogger,
		maxSteps:     maxSteps,
		allowExec:    allowExec,
		allowWeb:     allowWeb,
		allowBrowser: allowBrowser,
	}
}

// InvokeSkill runs the named skill as a child agent and returns its
// final text result. The user query (task) is substituted into
// $ARGUMENTS in the skill body and also passed as the child's user
// message.
func (r *cliSkillRunner) InvokeSkill(ctx context.Context, name, task string) (string, error) {
	s := skills.Find(r.skills, name)
	if s == nil {
		return "", fmt.Errorf("unknown skill %q", name)
	}
	for _, t := range s.Tools {
		if !config.ValidAgentTool(t) {
			return "", fmt.Errorf("skill %q: invalid tool name %q", name, t)
		}
	}

	systemPrompt, err := skills.PrepareBody(s.Body, task, r.refs)
	if err != nil {
		return "", fmt.Errorf("skill %q: %w", name, err)
	}

	var childTools []llm.ToolDef
	if len(s.Tools) > 0 {
		resolved, err := tools.ResolveToolNamesWithPolicy(s.Tools, r.allowExec, r.allowWeb, r.allowBrowser)
		if err != nil {
			return "", fmt.Errorf("skill %q: resolve tools: %w", name, err)
		}
		childTools = resolved
	} else {
		childTools = tools.ListToolsForChild()
	}

	childClient := r.baseClient
	overridden := false
	if s.Provider != "" {
		provCfg, ok := r.cfg.FindProvider(s.Provider)
		if !ok {
			return "", fmt.Errorf("skill %q: provider %q not found in providers: section", name, s.Provider)
		}
		if s.Model != "" {
			provCfg.Model = s.Model
		}
		childClient = llm.NewClient(provCfg)
		overridden = true
	} else if s.Model != "" {
		overrideCfg := r.cfg.LLM
		overrideCfg.Model = s.Model
		childClient = llm.NewClient(overrideCfg)
		overridden = true
	}
	// Only attach the logger when we built a fresh override client. The shared
	// baseClient already has its logger set once at construction time; calling
	// SetLogger on it here is a plain pointer write that races with any
	// concurrent caller of the same client (e.g. parallel workflow cohorts).
	if overridden {
		if oc, ok := childClient.(*llm.OpenAIClient); ok && r.agentLogger != nil {
			oc.SetLogger(r.agentLogger)
		}
	}

	ag, err := agent.New(childClient, r.validator, r.toolRunner, agent.Options{
		MaxSteps:             r.maxSteps,
		AllowExec:            r.allowExec,
		AllowWeb:             r.allowWeb,
		AllowBrowser:         r.allowBrowser,
		CustomTools:          childTools,
		SystemPromptOverride: systemPrompt,
		IsChild:              true,
		// SubtaskRunner intentionally nil: no recursive spawning.
		// SkillRunner intentionally nil: no recursive skill_invoke.
	})
	if err != nil {
		return "", fmt.Errorf("skill %q: %w", name, err)
	}
	_, res, runErr := ag.Run(ctx, nil, task)
	if runErr != nil {
		return "", fmt.Errorf("skill %q: %w", name, runErr)
	}
	if res.SubtaskResult != "" {
		return res.SubtaskResult, nil
	}
	return fmt.Sprintf("skill %q completed with %d patch(es)", name, len(res.Patches)), nil
}

// skillSpecs converts discovered skills into agent.SkillSpec metadata
// for system-prompt advertisement.
func skillSpecs(ss []*skills.Skill) []agent.SkillSpec {
	out := make([]agent.SkillSpec, len(ss))
	for i, s := range ss {
		out[i] = agent.SkillSpec{Name: s.Name, Description: s.Description}
	}
	return out
}
