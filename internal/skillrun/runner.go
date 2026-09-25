// Package skillrun runs file-based skills as synchronous child agents.
package skillrun

import (
	"context"
	"fmt"
	"strings"

	agenthistory "github.com/orchestra/orchestra/internal/agent/history"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/app"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/skills"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// Config is what a Runner runs skills with.
//
// Required: Cfg, Skills, Client, Validator, Runner. The rest is optional.
type Config struct {
	Cfg    *config.ProjectConfig
	Skills []*skills.Skill
	Refs   map[string]string

	// Client runs a skill that names no provider or model of its own.
	Client llm.Client
	// FixedClient keeps every skill on Client, whatever provider or model it
	// names: the client a test injected.
	FixedClient bool
	Validator   *schema.Validator
	Runner      *tools.Runner

	AgentLogger *llm.Logger
	// MaxSteps bounds a skill's child; 0 or less is 12.
	MaxSteps int

	AllowExec    bool
	AllowWeb     bool
	AllowBrowser bool

	// PermissionRequester, if non-nil, puts a child's exec.run to the user.
	PermissionRequester agent.PermissionRequester
	// Events is where a child's lifecycle and stream go.
	Events app.ChildEvents
}

// Runner implements agent.SkillRunner.
type Runner struct {
	c Config
}

// New builds a SkillRunner.
func New(c Config) *Runner {
	if c.MaxSteps <= 0 {
		c.MaxSteps = 12
	}
	return &Runner{c: c}
}

// Specs converts discovered skills into agent.SkillSpec metadata.
func Specs(ss []*skills.Skill) []agent.SkillSpec {
	out := make([]agent.SkillSpec, len(ss))
	for i, s := range ss {
		out[i] = agent.SkillSpec{Name: s.Name, Description: s.Description}
	}
	return out
}

// Outcome is what a skill's child produced.
type Outcome struct {
	// Text is the child's answer: its task_result, else its closing words.
	Text string
	// Report is the answer as a parent agent reads it: the task_result as
	// it is, else the files the child touched with its closing words.
	Report string
	Steps  int
}

// InvokeSkill runs the named skill as a child agent and returns what a parent
// agent reads back from skill_invoke.
func (r *Runner) InvokeSkill(ctx context.Context, name, task string) (string, error) {
	out, err := r.Run(ctx, name, task)
	if err != nil {
		return "", err
	}
	return out.Report, nil
}

// Run runs the named skill as a child agent: its body as the system prompt,
// its tools as the tool list, its provider or model when it names one, on a
// layer of its own that commits when the child succeeds. An empty task is a
// normal way to run a command — its body carries the instructions — and the
// child is asked to run it.
func (r *Runner) Run(ctx context.Context, name, task string) (*Outcome, error) {
	c := r.c
	s := skills.Find(c.Skills, name)
	if s == nil {
		return nil, fmt.Errorf("unknown skill %q", name)
	}
	if c.Cfg != nil {
		if err := c.Cfg.CheckSkillNameFree(s.Name); err != nil {
			return nil, err
		}
	}
	for _, t := range s.Tools {
		if !config.ValidAgentTool(t) {
			return nil, fmt.Errorf("skill %q: invalid tool name %q", name, t)
		}
	}

	systemPrompt, err := skills.PrepareBody(s.Body, task, c.Refs)
	if err != nil {
		return nil, fmt.Errorf("skill %q: %w", name, err)
	}

	var childTools []llm.ToolDef
	if len(s.Tools) > 0 {
		resolved, err := tools.ResolveToolNamesWithPolicy(s.Tools, tools.Capabilities{
			Exec:    c.AllowExec,
			Web:     c.AllowWeb,
			Browser: c.AllowBrowser,
		})
		if err != nil {
			return nil, fmt.Errorf("skill %q: resolve tools: %w", name, err)
		}
		// When policy removes every tool a skill asked for (a skill whose
		// only tools are bash and git.commit, run without --allow-exec), a
		// child with no way to act burns its retries and ends with an
		// opaque error. The operator gets the cause instead.
		if len(resolved) == 0 {
			return nil, fmt.Errorf("skill %q: every tool in `tools: %v` is disabled by current policy "+
				"(allow_exec=%v allow_web=%v allow_browser=%v) — re-run with the appropriate --allow-* flag or drop the gated tools from the skill",
				name, s.Tools, c.AllowExec, c.AllowWeb, c.AllowBrowser)
		}
		childTools = resolved
	} else {
		childTools = tools.ListToolsForChild()
	}

	childClient := c.Client
	if (s.Provider != "" || s.Model != "") && !c.FixedClient {
		if c.Cfg == nil {
			return nil, fmt.Errorf("skill %q names a provider or model, but there is no project config to find it in", name)
		}
		client, _, err := app.ClientFor(c.Cfg, s.Provider, s.Model, c.AgentLogger)
		if err != nil {
			return nil, fmt.Errorf("skill %q: %w", name, err)
		}
		childClient = client
	}

	// The project's budgets, breakers, step timeout and permission rules, as
	// every other child gets them. A skill child used to run with none: the
	// agent's 25-second step timeout, no prompt budget, no deny rules.
	settings := app.SettingsFrom(c.Cfg)
	opts := app.ChildOptions(&settings, func(o *agent.Options) {
		o.MaxSteps = c.MaxSteps
		o.AllowExec = c.AllowExec
		o.AllowWeb = c.AllowWeb
		o.AllowBrowser = c.AllowBrowser
		o.CustomTools = childTools
		o.SystemPromptOverride = systemPrompt
		o.AgentLogger = c.AgentLogger
		o.ModelLabel = childModel(c.Cfg, s.Provider, s.Model)
		o.PermissionRequester = c.PermissionRequester
		agent.ApplyHistoryConfig(o, c.Cfg)
	})

	// The skill's body is the instruction (it is the system prompt), but the
	// agent still needs a user turn to answer; a command run with nothing
	// after its name gets one that says what was asked.
	query := strings.TrimSpace(task)
	if query == "" {
		query = "Run the /" + s.Name + " command."
	}
	out, err := app.RunChild(ctx, app.ChildRun{
		Client:    childClient,
		Validator: c.Validator,
		Tools:     c.Runner,
		Options:   opts,
		Goal:      query,
		Kind:      "skill:" + s.Name,
		Events:    c.Events,
	})
	if err != nil {
		return nil, fmt.Errorf("skill %q: %w", name, err)
	}
	text := out.Text()
	report := text
	if out.Result == nil || out.Result.SubtaskResult == "" {
		// A skill's child usually works through edit/write and closes with
		// prose, not with task.result and not with final.patches. Reporting
		// only the patch count then told the parent "completed with 0
		// patch(es)" about a child that had just changed the file — and the
		// parent, told that nothing happened, invoked the skill again. Seen
		// live with the 27B on skill_does_the_work: retryLimit went 3 → 13 →
		// 53 → … → 218453 in eight calls, each one correct on its own. So the
		// parent gets what the child did: the files it touched and its
		// closing words.
		digest := 0
		if c.Cfg != nil {
			digest = c.Cfg.Agent.ResolvedToolDigestBytes()
		}
		report = skillReport(s.Name, task, out, digest)
	}
	return &Outcome{Text: text, Report: report, Steps: out.Steps()}, nil
}

// skillReport is what the parent reads back from skill_invoke when the child
// did not answer through task.result: the same structured summary a task
// child gets (goal, findings, files touched), with the child's own closing
// text — or, failing that, its patch count — as the result line.
func skillReport(name, task string, out *app.ChildOutcome, digestBudget int) string {
	closing := app.ClosingText(out.History)
	if closing == "" && out.Result != nil && len(out.Result.Patches) > 0 {
		closing = fmt.Sprintf("completed with %d patch(es)", len(out.Result.Patches))
	}
	if closing == "" {
		closing = "completed; see the files touched above"
	}
	return agenthistory.FormatSubagentResult("skill:"+name, task, out.History, closing, digestBudget)
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
