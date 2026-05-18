package core

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/skills"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/internal/workflow"
)

// --- workflow.list ---

type WorkflowListParams struct{}

type WorkflowListResult struct {
	Workflows []WorkflowSummary `json:"workflows"`
}

type WorkflowSummary struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Stages      []string `json:"stages"`
	Source      string   `json:"source,omitempty"`
}

func (c *Core) WorkflowList(_ WorkflowListParams) (*WorkflowListResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	ws, err := workflow.Discover(c.workspaceRoot)
	if err != nil {
		return nil, err
	}
	out := make([]WorkflowSummary, 0, len(ws))
	for _, w := range ws {
		ids := make([]string, len(w.Stages))
		for i, s := range w.Stages {
			ids[i] = s.ID
		}
		out = append(out, WorkflowSummary{
			Name:        w.Name,
			Description: w.Description,
			Stages:      ids,
			Source:      w.Source,
		})
	}
	return &WorkflowListResult{Workflows: out}, nil
}

// --- workflow.run ---

type WorkflowRunParams struct {
	Name         string `json:"name"`
	Arguments    string `json:"arguments"`
	Apply        bool   `json:"apply,omitempty"`
	AllowExec    bool   `json:"allow_exec,omitempty"`
	AllowWeb     bool   `json:"allow_web,omitempty"`
	AllowBrowser bool   `json:"allow_browser,omitempty"`

	// OnEvent receives streaming events. Set programmatically by the RPC
	// handler; not serialised. Events:
	//   "workflow.stage_start" {name, stage_id, attempt}
	//   "workflow.stage_done"  {name, stage_id, attempt, marker, action, output_kb}
	OnEvent func(method string, params any) `json:"-"`

	// PermissionRequester, if non-nil, gates exec.run interactively. Set
	// programmatically by the RPC handler.
	PermissionRequester PermissionRequester `json:"-"`
}

type WorkflowRunResult struct {
	Name          string            `json:"name"`
	Outputs       map[string]string `json:"outputs"`
	FinalStage    string            `json:"final_stage,omitempty"`
	FailureReason string            `json:"failure_reason,omitempty"`
	Stages        []StageRecord     `json:"stages"`
	DurationMS    int64             `json:"duration_ms"`
}

type StageRecord struct {
	StageID  string `json:"stage_id"`
	Attempt  int    `json:"attempt"`
	Marker   string `json:"marker,omitempty"`
	Action   string `json:"action"`
	OutputKB int    `json:"output_kb"`
}

func (c *Core) WorkflowRun(ctx context.Context, params WorkflowRunParams) (*WorkflowRunResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	if strings.TrimSpace(params.Name) == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "workflow name is empty", nil)
	}
	if strings.TrimSpace(params.Arguments) == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "workflow arguments are empty", nil)
	}

	ws, err := workflow.Discover(c.workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("discover workflows: %w", err)
	}
	w := workflow.Find(ws, params.Name)
	if w == nil {
		return nil, protocol.NewError(protocol.InvalidLLMOutput,
			fmt.Sprintf("workflow %q not found", params.Name), nil)
	}

	discoveredSkills, err := skills.Discover(c.workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("discover skills: %w", err)
	}
	discoveredRefs, err := skills.DiscoverRefs(c.workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("discover refs: %w", err)
	}
	for _, stage := range w.Stages {
		if skills.Find(discoveredSkills, stage.Skill) == nil {
			return nil, protocol.NewError(protocol.InvalidLLMOutput,
				fmt.Sprintf("workflow %q: stage %q references unknown skill %q", w.Name, stage.ID, stage.Skill), nil)
		}
	}

	allowExec := params.AllowExec
	if c.cfg != nil && c.cfg.Exec.Confirm != nil && !*c.cfg.Exec.Confirm {
		allowExec = true
	}

	inv := &coreStageInvoker{
		core:         c,
		skills:       discoveredSkills,
		refs:         discoveredRefs,
		allowExec:    allowExec,
		allowWeb:     params.AllowWeb,
		allowBrowser: params.AllowBrowser,
		permReq:      params.PermissionRequester,
	}

	markersFor := func(skillName string) []string {
		s := skills.Find(discoveredSkills, skillName)
		if s == nil {
			return nil
		}
		return s.CompletionMarkers
	}

	emit := params.OnEvent
	if emit == nil {
		emit = func(string, any) {}
	}

	opts := workflow.RunOptions{
		Arguments: params.Arguments,
		OnStageStart: func(stageID string, attempt int) {
			emit("workflow.stage_start", map[string]any{
				"name":     w.Name,
				"stage_id": stageID,
				"attempt":  attempt,
			})
		},
		OnStageDone: func(stageID string, attempt int, output, marker, nextAction string) {
			emit("workflow.stage_done", map[string]any{
				"name":      w.Name,
				"stage_id":  stageID,
				"attempt":   attempt,
				"marker":    marker,
				"action":    nextAction,
				"output_kb": (len(output) + 1023) / 1024,
			})
		},
	}

	start := time.Now()
	res, runErr := workflow.Run(ctx, w, inv, markersFor, opts)
	elapsed := time.Since(start)

	out := &WorkflowRunResult{
		Name:       w.Name,
		Outputs:    map[string]string{},
		DurationMS: elapsed.Milliseconds(),
	}
	if res != nil {
		out.Outputs = res.Outputs
		out.FinalStage = res.FinalStage
		out.FailureReason = res.FailureReason
		out.Stages = make([]StageRecord, len(res.StagesExecuted))
		for i, s := range res.StagesExecuted {
			out.Stages[i] = StageRecord{
				StageID:  s.StageID,
				Attempt:  s.Attempt,
				Marker:   s.Marker,
				Action:   s.Action,
				OutputKB: s.OutputKB,
			}
		}
	}
	if runErr != nil {
		return out, runErr
	}
	return out, nil
}

// coreStageInvoker implements workflow.StageInvoker by spawning a fresh
// child agent per stage. Mirrors cli/workflow.go::workflowStageInvoker
// but uses Core's existing cfg/llmClient/validator/tools rather than
// allocating its own.
type coreStageInvoker struct {
	core         *Core
	skills       []*skills.Skill
	refs         map[string]string
	allowExec    bool
	allowWeb     bool
	allowBrowser bool
	permReq      PermissionRequester
}

func (inv *coreStageInvoker) Invoke(ctx context.Context, skillName, userQuery string) (string, error) {
	s := skills.Find(inv.skills, skillName)
	if s == nil {
		return "", fmt.Errorf("unknown skill %q", skillName)
	}
	for _, t := range s.Tools {
		if !config.ValidAgentTool(t) {
			return "", fmt.Errorf("skill %q: invalid tool name %q", skillName, t)
		}
	}

	systemPrompt, err := skills.PrepareBody(s.Body, userQuery, inv.refs)
	if err != nil {
		return "", fmt.Errorf("skill %q: %w", skillName, err)
	}

	var childTools []llm.ToolDef
	if len(s.Tools) > 0 {
		resolved, err := tools.ResolveToolNamesWithPolicy(s.Tools, inv.allowExec, inv.allowWeb, inv.allowBrowser)
		if err != nil {
			return "", fmt.Errorf("skill %q: resolve tools: %w", skillName, err)
		}
		childTools = resolved
	} else {
		childTools = tools.ListToolsForChild()
	}

	childClient := inv.core.llmClient
	if s.Provider != "" && inv.core.cfg != nil {
		provCfg, ok := inv.core.cfg.FindProvider(s.Provider)
		if !ok {
			return "", fmt.Errorf("skill %q: provider %q not found", skillName, s.Provider)
		}
		if s.Model != "" {
			provCfg.Model = s.Model
		}
		childClient = llm.NewClient(provCfg)
	} else if s.Model != "" && inv.core.cfg != nil {
		overrideCfg := inv.core.cfg.LLM
		overrideCfg.Model = s.Model
		childClient = llm.NewClient(overrideCfg)
	}

	maxSteps := 24
	if inv.core.cfg != nil && inv.core.cfg.Agent.MaxSteps > 0 {
		maxSteps = inv.core.cfg.Agent.MaxSteps
	}

	ag, err := agent.New(childClient, inv.core.validator, inv.core.tools, agent.Options{
		MaxSteps:             maxSteps,
		AllowExec:            inv.allowExec,
		AllowWeb:             inv.allowWeb,
		AllowBrowser:         inv.allowBrowser,
		CustomTools:          childTools,
		SystemPromptOverride: systemPrompt,
		PermissionRequester:  convertPermissionRequester(inv.permReq),
	})
	if err != nil {
		return "", fmt.Errorf("skill %q: %w", skillName, err)
	}
	history, res, runErr := ag.Run(ctx, nil, userQuery)
	if runErr != nil {
		return "", fmt.Errorf("skill %q: %w", skillName, runErr)
	}
	if res.SubtaskResult != "" {
		return res.SubtaskResult, nil
	}
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == llm.RoleAssistant && strings.TrimSpace(history[i].Content) != "" {
			return history[i].Content, nil
		}
	}
	return fmt.Sprintf("skill %q completed with %d patch(es)", skillName, len(res.Patches)), nil
}
