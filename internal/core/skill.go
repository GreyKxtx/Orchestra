package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/skills"
	"github.com/orchestra/orchestra/internal/tools"
)

// --- skill.list ---

type SkillListParams struct{}

type SkillListResult struct {
	Skills []SkillSummary `json:"skills"`
}

type SkillSummary struct {
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Tools             []string `json:"tools,omitempty"`
	Provider          string   `json:"provider,omitempty"`
	Model             string   `json:"model,omitempty"`
	CompletionMarkers []string `json:"completion_markers,omitempty"`
	Origin            string   `json:"origin,omitempty"`
}

func (c *Core) SkillList(_ SkillListParams) (*SkillListResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	ss, err := skills.Discover(c.workspaceRoot)
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

type SkillInvokeResult struct {
	Skill  string `json:"skill"`
	Output string `json:"output"`
	Marker string `json:"marker,omitempty"`
	Steps  int    `json:"steps"`
}

func (c *Core) SkillInvoke(ctx context.Context, params SkillInvokeParams) (*SkillInvokeResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	if strings.TrimSpace(params.Name) == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "skill name is empty", nil)
	}
	if strings.TrimSpace(params.Arguments) == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "skill arguments are empty", nil)
	}

	ss, err := skills.Discover(c.workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("discover skills: %w", err)
	}
	s := skills.Find(ss, params.Name)
	if s == nil {
		return nil, protocol.NewError(protocol.InvalidLLMOutput,
			fmt.Sprintf("skill %q not found", params.Name), nil)
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
		resolved, err := tools.ResolveToolNamesWithPolicy(s.Tools, allowExec, params.AllowWeb, params.AllowBrowser)
		if err != nil {
			return nil, fmt.Errorf("skill %q: resolve tools: %w", params.Name, err)
		}
		childTools = resolved
	} else {
		childTools = tools.ListToolsForChild()
	}

	childClient := c.llmClient
	if s.Provider != "" && c.cfg != nil {
		provCfg, ok := c.cfg.FindProvider(s.Provider)
		if !ok {
			return nil, fmt.Errorf("skill %q: provider %q not found", params.Name, s.Provider)
		}
		if s.Model != "" {
			provCfg.Model = s.Model
		}
		childClient = llm.NewClient(provCfg)
	} else if s.Model != "" && c.cfg != nil {
		overrideCfg := c.cfg.LLM
		overrideCfg.Model = s.Model
		childClient = llm.NewClient(overrideCfg)
	}

	maxSteps := 24
	if c.cfg != nil && c.cfg.Agent.MaxSteps > 0 {
		maxSteps = c.cfg.Agent.MaxSteps
	}

	ag, err := agent.New(childClient, c.validator, c.tools, agent.Options{
		MaxSteps:             maxSteps,
		AllowExec:            allowExec,
		AllowWeb:             params.AllowWeb,
		AllowBrowser:         params.AllowBrowser,
		CustomTools:          childTools,
		SystemPromptOverride: systemPrompt,
		PermissionRequester:  convertPermissionRequester(params.PermissionRequester),
	})
	if err != nil {
		return nil, fmt.Errorf("skill %q: %w", params.Name, err)
	}
	history, res, runErr := ag.Run(ctx, nil, params.Arguments)
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
