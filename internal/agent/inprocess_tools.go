package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/tools"
)

func (a *Agent) handleTodoTool(name string, input json.RawMessage) (json.RawMessage, error) {
	switch name {
	case "todowrite":
		var req tools.TodoWriteRequest
		if err := json.Unmarshal(input, &req); err != nil {
			return nil, fmt.Errorf("todo.write: invalid input: %w", err)
		}
		normalized, err := tools.ValidateTodos(req.Todos)
		if err != nil {
			return nil, fmt.Errorf("todo.write: %w", err)
		}
		a.todos = normalized
		resp, _ := json.Marshal(tools.TodoWriteResponse{Count: len(normalized)})
		return resp, nil
	case "todoread":
		resp, _ := json.Marshal(tools.TodoReadResponse{Todos: a.todos})
		return resp, nil
	default:
		return nil, fmt.Errorf("unknown todo tool: %s", name)
	}
}

// renderTodosBlock returns a formatted todo block for injection into the user prompt.
// Returns empty string when todos is empty.
func renderTodosBlock(todos []tools.TodoItem) string {
	if len(todos) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<todo_list>\n")
	for _, item := range todos {
		b.WriteString(fmt.Sprintf("- [%s] %s (id: %s)\n", item.Status, item.Content, item.ID))
	}
	b.WriteString("</todo_list>\n")
	return b.String()
}

// handleSkillInvoke handles skill_invoke in-process via SkillRunner.
// Validates the requested skill name against Options.Skills before running.
func (a *Agent) handleSkillInvoke(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Skill string `json:"skill"`
		Task  string `json:"task"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, fmt.Errorf("skill_invoke: invalid input: %w", err)
	}
	if strings.TrimSpace(req.Skill) == "" {
		return nil, fmt.Errorf("skill_invoke: skill is required")
	}
	if strings.TrimSpace(req.Task) == "" {
		return nil, fmt.Errorf("skill_invoke: task is required")
	}
	known := false
	for _, s := range a.opts.Skills {
		if s.Name == req.Skill {
			known = true
			break
		}
	}
	if !known {
		return nil, fmt.Errorf("skill_invoke: unknown skill %q", req.Skill)
	}
	result, err := a.opts.SkillRunner.InvokeSkill(ctx, req.Skill, req.Task)
	if err != nil {
		return nil, fmt.Errorf("skill_invoke: %w", err)
	}
	resp, _ := json.Marshal(map[string]any{
		"skill":  req.Skill,
		"status": "done",
		"result": result,
	})
	return resp, nil
}

// handleTaskTool handles task / task.spawn / task.wait / task.cancel in-process via SubtaskRunner.
func (a *Agent) handleTaskTool(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	switch name {
	case "task":
		var req struct {
			Description  string `json:"description"`
			Prompt       string `json:"prompt"`
			Goal         string `json:"goal"`
			SubagentType string `json:"subagent_type"`
			Tier         string `json:"tier"`
			Provider     string `json:"provider"`
			Model        string `json:"model"`
			MaxSteps     int    `json:"max_steps"`
			TimeoutMS    int    `json:"timeout_ms"`
		}
		if err := json.Unmarshal(input, &req); err != nil {
			return nil, fmt.Errorf("task: invalid input: %w", err)
		}
		goal := strings.TrimSpace(req.Prompt)
		if goal == "" {
			goal = strings.TrimSpace(req.Goal)
		}
		if goal == "" {
			return nil, fmt.Errorf("task: prompt is required")
		}
		subagentType := strings.TrimSpace(req.SubagentType)
		if subagentType == "" {
			subagentType = "explore"
		}
		timeoutMS := req.TimeoutMS
		if timeoutMS <= 0 {
			timeoutMS = 120_000
		}
		taskID, err := a.opts.SubtaskRunner.Spawn(ctx, SubtaskSpawnRequest{
			Goal:         goal,
			SubagentType: subagentType,
			Tier:         strings.TrimSpace(req.Tier),
			Provider:     strings.TrimSpace(req.Provider),
			Model:        strings.TrimSpace(req.Model),
			MaxSteps:     req.MaxSteps,
			TimeoutMS:    timeoutMS,
		})
		if err != nil {
			return nil, fmt.Errorf("task: spawn: %w", err)
		}
		result, err := a.opts.SubtaskRunner.Wait(ctx, taskID, timeoutMS)
		if err != nil {
			return nil, fmt.Errorf("task: wait: %w", err)
		}
		out := map[string]any{
			"task_id": taskID,
			"status":  result.Status,
		}
		if req.Description != "" {
			out["description"] = req.Description
		}
		if result.Result != "" {
			out["result"] = result.Result
		}
		if result.Error != "" {
			out["error"] = result.Error
		}
		resp, _ := json.Marshal(out)
		return resp, nil

	case "task_spawn":
		var req struct {
			Goal         string `json:"goal"`
			Prompt       string `json:"prompt"`
			SubagentType string `json:"subagent_type"`
			Tier         string `json:"tier"`
			Provider     string `json:"provider"`
			Model        string `json:"model"`
			MaxSteps     int    `json:"max_steps"`
			TimeoutMS    int    `json:"timeout_ms"`
		}
		if err := json.Unmarshal(input, &req); err != nil {
			return nil, fmt.Errorf("task.spawn: invalid input: %w", err)
		}
		goal := strings.TrimSpace(req.Goal)
		if goal == "" {
			goal = strings.TrimSpace(req.Prompt)
		}
		if goal == "" {
			return nil, fmt.Errorf("task.spawn: goal is required")
		}
		subagentType := strings.TrimSpace(req.SubagentType)
		if subagentType == "" {
			subagentType = "explore"
		}
		timeoutMS := req.TimeoutMS
		if timeoutMS <= 0 {
			timeoutMS = 120_000 // same default as sync task — avoid orphan children
		}
		taskID, err := a.opts.SubtaskRunner.Spawn(ctx, SubtaskSpawnRequest{
			Goal:         goal,
			SubagentType: subagentType,
			Tier:         strings.TrimSpace(req.Tier),
			Provider:     strings.TrimSpace(req.Provider),
			Model:        strings.TrimSpace(req.Model),
			MaxSteps:     req.MaxSteps,
			TimeoutMS:    timeoutMS,
		})
		if err != nil {
			return nil, fmt.Errorf("task.spawn: %w", err)
		}
		resp, _ := json.Marshal(map[string]any{"task_id": taskID, "status": "spawned"})
		return resp, nil

	case "task_wait":
		var req struct {
			TaskID    string `json:"task_id"`
			TimeoutMS int    `json:"timeout_ms"`
		}
		if err := json.Unmarshal(input, &req); err != nil {
			return nil, fmt.Errorf("task.wait: invalid input: %w", err)
		}
		if strings.TrimSpace(req.TaskID) == "" {
			return nil, fmt.Errorf("task.wait: task_id is required")
		}
		result, err := a.opts.SubtaskRunner.Wait(ctx, req.TaskID, req.TimeoutMS)
		if err != nil {
			return nil, fmt.Errorf("task.wait: %w", err)
		}
		resp, _ := json.Marshal(result)
		return resp, nil

	case "task_cancel":
		var req struct {
			TaskID string `json:"task_id"`
		}
		if err := json.Unmarshal(input, &req); err != nil {
			return nil, fmt.Errorf("task.cancel: invalid input: %w", err)
		}
		if strings.TrimSpace(req.TaskID) == "" {
			return nil, fmt.Errorf("task.cancel: task_id is required")
		}
		if err := a.opts.SubtaskRunner.Cancel(ctx, req.TaskID); err != nil {
			return nil, fmt.Errorf("task.cancel: %w", err)
		}
		resp, _ := json.Marshal(map[string]any{"task_id": req.TaskID, "status": "cancelled"})
		return resp, nil

	default:
		return nil, fmt.Errorf("unknown task tool: %s", name)
	}
}
