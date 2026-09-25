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

// notifyChildDone announces the end of e. Like child_started it names the
// task's parent and depth, so the delegation tree can be rebuilt from the
// event log alone.
func (r *TaskRunner) notifyChildDone(e *taskEntry, parentToolCallID, subagentType string, result *agent.SubtaskResult) {
	if r == nil || r.child.NotifyAgentEvent == nil || e == nil {
		return
	}
	if result == nil {
		result = &agent.SubtaskResult{TaskID: e.id, Status: "error", Error: "task ended without a result"}
	}
	params := map[string]any{
		"type":                "child_done",
		"task_id":             e.id,
		"parent_tool_call_id": parentToolCallID,
		"subagent_type":       subagentType,
		"status":              result.Status,
		"depth":               e.depth,
	}
	if e.parentTaskID != "" {
		params["parent_task_id"] = e.parentTaskID
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
