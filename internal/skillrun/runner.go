// Package skillrun runs file-based skills as synchronous child agents.
package skillrun

import (
	"context"
	"fmt"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
	"github.com/orchestra/orchestra/internal/skills"
	"github.com/orchestra/orchestra/internal/tools"
)

// Runner implements agent.SkillRunner.
type Runner struct {
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

// New builds a SkillRunner. maxSteps <= 0 defaults to 12.
func New(
	cfg *config.ProjectConfig,
	discovered []*skills.Skill,
	refs map[string]string,
	baseClient llm.Client,
	validator *schema.Validator,
	toolRunner *tools.Runner,
	agentLogger *llm.Logger,
	maxSteps int,
	allowExec, allowWeb, allowBrowser bool,
) *Runner {
	if maxSteps <= 0 {
		maxSteps = 12
	}
	return &Runner{
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

// Specs converts discovered skills into agent.SkillSpec metadata.
func Specs(ss []*skills.Skill) []agent.SkillSpec {
	out := make([]agent.SkillSpec, len(ss))
	for i, s := range ss {
		out[i] = agent.SkillSpec{Name: s.Name, Description: s.Description}
	}
	return out
}

// InvokeSkill runs the named skill as a child agent and returns its result text.
func (r *Runner) InvokeSkill(ctx context.Context, name, task string) (string, error) {
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
		resolved, err := tools.ResolveToolNamesWithPolicy(s.Tools, tools.Capabilities{
			Exec:    r.allowExec,
			Web:     r.allowWeb,
			Browser: r.allowBrowser,
		})
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
	if overridden {
		if oc, ok := llm.AsOpenAIClient(childClient); ok && r.agentLogger != nil {
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
