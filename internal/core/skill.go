package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/memory"
	"github.com/orchestra/orchestra/internal/skillrun"
	"github.com/orchestra/orchestra/internal/skills"
	"github.com/orchestra/orchestra/internal/tools"
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
	refs, err := skills.DiscoverRefs(c.workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("discover refs: %w", err)
	}

	allowExec := params.AllowExec
	if c.cfg != nil && c.cfg.Exec.Confirm != nil && !*c.cfg.Exec.Confirm {
		allowExec = true
	}

	// The same runner skill_invoke goes through inside a turn: one door, one
	// set of budgets, tools and rules. The client sees the skill as a child of
	// this call — its start, its stream, its end.
	events := childEventsFor(params.OnEvent, EventEnvelope{TurnID: NewTurnID()})
	runner := skillrun.New(skillrun.Config{
		Cfg:                 c.cfg,
		Skills:              ss,
		Refs:                refs,
		Client:              c.llmClient,
		FixedClient:         c.llmClientInjected,
		Validator:           c.validator,
		Runner:              c.tools,
		MaxSteps:            c.cfg.Agent.MaxSteps,
		AllowExec:           allowExec,
		AllowWeb:            params.AllowWeb,
		AllowBrowser:        params.AllowBrowser,
		PermissionRequester: convertPermissionRequester(params.PermissionRequester),
		Events:              events,
	})

	// skill.invoke is always a preview (it has no Apply parameter), on a
	// turn of its own. runMu is held shared.
	c.runMu.RLock()
	defer c.runMu.RUnlock()
	turn := c.tools.NewTurn(tools.TurnOptions{DryRun: true, Memory: memory.ConfigFrom(c.cfg.Memory)})
	defer turn.Close()
	ctx = tools.WithTurn(ctx, turn)

	res, err := runner.Run(ctx, params.Name, params.Arguments)
	if err != nil {
		return nil, err
	}
	out := &SkillInvokeResult{
		Skill:  params.Name,
		Steps:  res.Steps,
		Output: res.Text,
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
