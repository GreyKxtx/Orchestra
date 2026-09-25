package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/app"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/memory"
	"github.com/orchestra/orchestra/internal/skills"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/protocol/wire"
)

// --- skill.list ---

func (c *Core) SkillList(_ SkillListParams) (*SkillListResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	ss, err := skills.DiscoverCached(c.workspaceRoot)
	if err != nil {
		return nil, err
	}
	out := make([]SkillSummary, 0, len(ss))
	for _, s := range ss {
		out = append(out, SkillSummary{
			Name:              s.Name,
			Description:       s.Description,
			Tools:             s.Tools,
			Provider:          s.Provider,
			Model:             s.Model,
			CompletionMarkers: s.CompletionMarkers,
			Origin:            s.Origin,
		})
	}
	return &SkillListResult{Skills: out}, nil
}

// --- skill.invoke ---

type SkillInvokeParams struct {
	Name         string `json:"name"`
	Arguments    string `json:"arguments"`
	AllowExec    bool   `json:"allow_exec,omitempty"`
	AllowWeb     bool   `json:"allow_web,omitempty"`
	AllowBrowser bool   `json:"allow_browser,omitempty"`

	// OnEvent receives streaming events from the child agent. Set
	// programmatically by the RPC handler.
	OnEvent func(method string, params any) `json:"-"`

	// PermissionRequester, if non-nil, gates exec.run interactively.
	PermissionRequester PermissionRequester `json:"-"`
}

func (c *Core) SkillInvoke(ctx context.Context, params SkillInvokeParams) (*SkillInvokeResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	if strings.TrimSpace(params.Name) == "" {
		return nil, protocol.NewError(protocol.InvalidParams, "skill name is empty", nil)
	}
	// No argument is a normal way to run a command: a command file carries its
	// own instructions, and $ARGUMENTS is what the person typed after the name
	// — often nothing at all (.claude/commands/*.md is written that way).
	// Refusing an empty one made every such command fail before it started.

	ss, err := skills.DiscoverCached(c.workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("discover skills: %w", err)
	}
	s := skills.Find(ss, params.Name)
	if s == nil {
		return nil, protocol.NewError(protocol.NotFound,
			fmt.Sprintf("skill %q not found", params.Name), nil)
	}
	if err := c.cfg.CheckSkillNameFree(s.Name); err != nil {
		return nil, protocol.NewError(protocol.InvalidParams, err.Error(), nil)
	}
	for _, t := range s.Tools {
		if !config.ValidAgentTool(t) {
			return nil, fmt.Errorf("skill %q: invalid tool name %q", params.Name, t)
		}
	}

	refs, err := skills.DiscoverRefs(c.workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("discover refs: %w", err)
	}
	systemPrompt, err := skills.PrepareBody(s.Body, params.Arguments, refs)
	if err != nil {
		return nil, fmt.Errorf("skill %q: %w", params.Name, err)
	}

	allowExec := params.AllowExec
	if c.cfg != nil && c.cfg.Exec.Confirm != nil && !*c.cfg.Exec.Confirm {
		allowExec = true
	}

	var childTools []llm.ToolDef
	if len(s.Tools) > 0 {
		resolved, err := tools.ResolveToolNamesWithPolicy(s.Tools, tools.Capabilities{
			Exec:    allowExec,
			Web:     params.AllowWeb,
			Browser: params.AllowBrowser,
		})
		if err != nil {
			return nil, fmt.Errorf("skill %q: resolve tools: %w", params.Name, err)
		}
		// Empty after policy filter → skill is unusable as configured. Fail
		// loud rather than starve the agent. See stageinvoke.Invoke for the
		// same guard on the workflow path.
		if len(resolved) == 0 {
			return nil, fmt.Errorf("skill %q: every tool in `tools: %v` is disabled by current policy "+
				"(allow_exec=%v allow_web=%v allow_browser=%v)",
				params.Name, s.Tools, allowExec, params.AllowWeb, params.AllowBrowser)
		}
		childTools = resolved
	} else {
		childTools = tools.ListToolsForChild()
	}

	childClient := c.llmClient
	if (s.Provider != "" || s.Model != "") && c.cfg != nil && !c.llmClientInjected {
		client, _, err := app.ClientFor(c.cfg, s.Provider, s.Model, nil)
		if err != nil {
			return nil, fmt.Errorf("skill %q: %w", params.Name, err)
		}
		childClient = client
	}

	// The project's budgets, breakers, step timeout (llm.timeout_s bounds each
	// model step, as on every other run path) and permission rules.
	settings := app.SettingsFrom(c.cfg)
	agOpts := app.ChildOptions(&settings, func(o *agent.Options) {
		if o.MaxSteps <= 0 {
			o.MaxSteps = 24
		}
		o.AllowExec = allowExec
		o.AllowWeb = params.AllowWeb
		o.AllowBrowser = params.AllowBrowser
		o.CustomTools = childTools
		o.SystemPromptOverride = systemPrompt
		o.PermissionRequester = convertPermissionRequester(params.PermissionRequester)
		agent.ApplyHistoryConfig(o, c.cfg)
	})
	ag, err := agent.New(childClient, c.validator, c.tools, agOpts)
	if err != nil {
		return nil, fmt.Errorf("skill %q: %w", params.Name, err)
	}

	// skill.invoke is always a preview (it has no Apply parameter), on a
	// turn of its own. runMu is held shared.
	c.runMu.RLock()
	defer c.runMu.RUnlock()
	turn := c.tools.NewTurn(tools.TurnOptions{DryRun: true, Memory: memory.ConfigFrom(c.cfg.Memory)})
	defer turn.Close()
	ctx = tools.WithTurn(ctx, turn)

	// The command's own body is the instruction (it is the system prompt), but
	// the agent still needs a user turn to answer; a command run with nothing
	// after its name gets one that says what was asked.
	query := strings.TrimSpace(params.Arguments)
	if query == "" {
		query = "Run the /" + params.Name + " command."
	}
	history, res, runErr := ag.Run(ctx, nil, query)
	if runErr != nil {
		return nil, fmt.Errorf("skill %q: %w", params.Name, runErr)
	}

	out := &SkillInvokeResult{
		Skill: params.Name,
		Steps: res.Steps,
	}
	switch {
	case res.SubtaskResult != "":
		out.Output = res.SubtaskResult
	default:
		for i := len(history) - 1; i >= 0; i-- {
			if history[i].Role == llm.RoleAssistant && strings.TrimSpace(history[i].Content) != "" {
				out.Output = history[i].Content
				break
			}
		}
	}
	out.Marker = detectSkillMarker(out.Output, s.CompletionMarkers)
	return out, nil
}

// detectSkillMarker scans the output for any line equal to a known marker.
func detectSkillMarker(output string, markers []string) string {
	if len(markers) == 0 {
		return ""
	}
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimRight(line, " \t\r")
		for _, m := range markers {
			if trimmed == m {
				return m
			}
		}
	}
	return ""
}

// The wire types of this file live in protocol/wire (ARCH-4); the aliases
// keep the package's names.
type (
	SkillInvokeResult = wire.SkillInvokeResult
	SkillListParams   = wire.SkillListParams
	SkillListResult   = wire.SkillListResult
	SkillSummary      = wire.SkillSummary
)
