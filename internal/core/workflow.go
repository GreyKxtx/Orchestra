package core

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/skills"
	"github.com/orchestra/orchestra/internal/stageinvoke"
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
	//   "workflow/stage_start" {name, stage_id, attempt}
	//   "workflow/stage_done"  {name, stage_id, attempt, marker, action, output_kb}
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
		return nil, protocol.NewError(protocol.InvalidParams, "workflow name is empty", nil)
	}
	if strings.TrimSpace(params.Arguments) == "" {
		return nil, protocol.NewError(protocol.InvalidParams, "workflow arguments are empty", nil)
	}

	ws, err := workflow.Discover(c.workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("discover workflows: %w", err)
	}
	w := workflow.Find(ws, params.Name)
	if w == nil {
		return nil, protocol.NewError(protocol.NotFound,
			fmt.Sprintf("workflow %q not found", params.Name), nil)
	}

	discoveredSkills, err := skills.DiscoverCached(c.workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("discover skills: %w", err)
	}
	discoveredRefs, err := skills.DiscoverRefs(c.workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("discover refs: %w", err)
	}
	for _, stage := range w.Stages {
		if skills.Find(discoveredSkills, stage.Skill) == nil {
			return nil, protocol.NewError(protocol.NotFound,
				fmt.Sprintf("workflow %q: stage %q references unknown skill %q", w.Name, stage.ID, stage.Skill), nil)
		}
	}
	if err := workflow.ValidateAgainstSkills(w, func(name string) []string {
		if s := skills.Find(discoveredSkills, name); s != nil {
			return s.CompletionMarkers
		}
		return nil
	}); err != nil {
		return nil, protocol.NewError(protocol.InvalidParams, err.Error(), nil)
	}

	allowExec := params.AllowExec
	if c.cfg != nil && c.cfg.Exec.Confirm != nil && !*c.cfg.Exec.Confirm {
		allowExec = true
	}

	inv := stageinvoke.New(stageinvoke.Config{
		Cfg:                 c.cfg,
		Skills:              discoveredSkills,
		Refs:                discoveredRefs,
		Client:              c.llmClient,
		Validator:           c.validator,
		Runner:              c.tools,
		AllowExec:           allowExec,
		AllowWeb:            params.AllowWeb,
		AllowBrowser:        params.AllowBrowser,
		PermissionRequester: convertPermissionRequester(params.PermissionRequester),
	})

	// Serialise against any concurrent agent.run / skill.invoke / ops.apply
	// that would otherwise race on the shared Runner's dry-run flag / staged
	// ops. Also wire params.Apply into runner-level staging so workflow
	// stages honour the dry-run/apply contract.
	//
	// Restore the prior dry-run flag on exit: WorkflowRun's Apply selection
	// is per-call, not a long-lived mode change. Without restore, a direct
	// tool.call right after a dry-run workflow would be spuriously blocked.
	c.runMu.Lock()
	prevDry := c.tools.DryRun()
	c.tools.SetDryRun(!params.Apply)
	c.tools.ClearStaged()
	defer func() {
		c.tools.SetDryRun(prevDry)
		c.runMu.Unlock()
	}()

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
			emit("workflow/stage_start", map[string]any{
				"name":     w.Name,
				"stage_id": stageID,
				"attempt":  attempt,
			})
		},
		OnStageDone: func(stageID string, attempt int, output, marker, nextAction string) {
			emit("workflow/stage_done", map[string]any{
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

