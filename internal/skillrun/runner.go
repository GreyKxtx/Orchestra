// Package skillrun runs file-based skills as synchronous child agents.
package skillrun

import (
	"context"
	"encoding/json"
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
	if err := r.cfg.CheckSkillNameFree(s.Name); err != nil {
		return "", err
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
	if s.Provider != "" || s.Model != "" {
		client, _, err := app.ClientFor(r.cfg, s.Provider, s.Model, r.agentLogger)
		if err != nil {
			return "", fmt.Errorf("skill %q: %w", name, err)
		}
		childClient = client
	}

	// The project's budgets, breakers, step timeout and permission rules, as
	// every other child gets them. A skill child used to run with none: the
	// agent's 25-second step timeout, no prompt budget, no deny rules.
	settings := app.SettingsFrom(r.cfg)
	ag, err := agent.New(childClient, r.validator, r.toolRunner, app.ChildOptions(&settings, func(o *agent.Options) {
		o.MaxSteps = r.maxSteps
		o.AllowExec = r.allowExec
		o.AllowWeb = r.allowWeb
		o.AllowBrowser = r.allowBrowser
		o.CustomTools = childTools
		o.SystemPromptOverride = systemPrompt
		o.AgentLogger = r.agentLogger
		o.ModelLabel = childModel(r.cfg, s.Provider, s.Model)
	}))
	if err != nil {
		return "", fmt.Errorf("skill %q: %w", name, err)
	}
	hist, res, runErr := ag.Run(ctx, nil, task)
	if runErr != nil {
		return "", fmt.Errorf("skill %q: %w", name, runErr)
	}
	if res != nil && res.SubtaskResult != "" {
		return res.SubtaskResult, nil
	}
	// A skill's child usually works through edit/write and closes with prose,
	// not with task.result and not with final.patches. Reporting only the
	// patch count then told the parent "completed with 0 patch(es)" about a
	// child that had just changed the file — and the parent, told that
	// nothing happened, invoked the skill again. Seen live with the 27B on
	// skill_does_the_work: retryLimit went 3 → 13 → 53 → … → 218453 in eight
	// calls, each one correct on its own. So the parent gets what the child
	// did: the files it touched and its closing words.
	return skillReport(name, task, hist, res, r.cfg.Agent.ResolvedToolDigestBytes()), nil
}

// skillReport is what the parent reads back from skill_invoke when the child
// did not answer through task.result: the same structured summary a task
// child gets (goal, findings, files touched), with the child's own closing
// text — or, failing that, its patch count — as the result line.
func skillReport(name, task string, hist []llm.Message, res *agent.Result, digestBudget int) string {
	closing := childClosingText(hist)
	if closing == "" && res != nil && len(res.Patches) > 0 {
		closing = fmt.Sprintf("completed with %d patch(es)", len(res.Patches))
	}
	if closing == "" {
		closing = "completed; see the files touched above"
	}
	return agent.FormatSubagentResult("skill:"+name, task, hist, closing, digestBudget)
}

// childClosingText is the child's last assistant message as prose. A final
// written in the {"type":"final","final":{...}} envelope is unwrapped to its
// summary, because the parent should read "raised retryLimit to 13", not the
// envelope around it.
func childClosingText(hist []llm.Message) string {
	for i := len(hist) - 1; i >= 0; i-- {
		m := hist[i]
		if m.Role != llm.RoleAssistant || strings.TrimSpace(m.Content) == "" {
			continue
		}
		text := strings.TrimSpace(m.Content)
		var env struct {
			Type  string `json:"type"`
			Final struct {
				Summary string `json:"summary"`
			} `json:"final"`
		}
		if json.Unmarshal([]byte(text), &env) == nil && env.Type == "final" {
			if s := strings.TrimSpace(env.Final.Summary); s != "" {
				return s
			}
			return ""
		}
		return text
	}
	return ""
}

// childModel is the model a skill child runs on: its own, its provider's, or
// the turn's. The prompt family follows it.
func childModel(cfg *config.ProjectConfig, provider, model string) string {
	if model != "" {
		return model
	}
	if cfg == nil {
		return ""
	}
	if provider != "" {
		if p, ok := cfg.FindProvider(provider); ok {
			return p.Model
		}
	}
	return cfg.LLM.Model
}
