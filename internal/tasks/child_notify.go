package tasks

import (
	"strings"
	"unicode/utf8"

	"github.com/orchestra/orchestra/internal/agent"
)

// PromoteHintsFromTaskResult extracts learning promote suggestions from task_result JSON.
func PromoteHintsFromTaskResult(taskResult string) (lessonHint, playbookHint string) {
	return extractPromoteHintForTest(taskResult), extractPlaybookPromoteHintForTest(taskResult)
}

func (r *TaskRunner) notifyChildDone(taskID, parentToolCallID, subagentType string, result *agent.SubtaskResult) {
	if r == nil || r.child.NotifyAgentEvent == nil || result == nil {
		return
	}
	params := map[string]any{
		"type":                "child_done",
		"task_id":             taskID,
		"parent_tool_call_id": parentToolCallID,
		"subagent_type":       subagentType,
		"status":              result.Status,
	}
	if result.Error != "" {
		params["error"] = result.Error
	}
	if summary := truncateChildSummary(result.Result); summary != "" {
		params["content"] = summary
	}
	lesson, playbook := PromoteHintsFromTaskResult(result.Result)
	if lesson != "" {
		params["lesson_promote_suggestion"] = lesson
	}
	if playbook != "" {
		params["playbook_promote_suggestion"] = playbook
	}
	r.child.NotifyAgentEvent(params)
}

func truncateChildSummary(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	const max = 240
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "…"
}
