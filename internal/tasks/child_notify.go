package tasks

import (
	"strings"
	"unicode/utf8"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/protocol/wire"
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
	ev := wire.AgentEvent{
		Type:             wire.EventChildDone,
		TaskID:           e.id,
		ParentToolCallID: parentToolCallID,
		ParentTaskID:     e.parentTaskID,
		SubagentType:     subagentType,
		Status:           result.Status,
		Depth:            e.depth,
		Error:            result.Error,
		Content:          truncateChildSummary(result.Result),
	}
	ev.LessonPromoteSuggestion, ev.PlaybookPromoteSuggestion = PromoteHintsFromTaskResult(result.Result)
	r.child.NotifyAgentEvent(ev)
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
