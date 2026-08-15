package tasks

import (
	"encoding/json"
	"strings"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/lessons"
	"github.com/orchestra/orchestra/internal/playbooks"
	"github.com/orchestra/orchestra/llm"
)

// recordWorkerLesson appends a rule-based episodic lesson after a worker child
// finishes (success or verify failure). Returns a promote hint when the same
// anti-pattern repeats PromoteSuggestThreshold times. Best-effort: errors ignored.
func recordWorkerLesson(projectRoot string, wo *WorkOrder, hist []llm.Message, taskResult, status string) string {
	if projectRoot == "" || wo == nil {
		return ""
	}
	dept := workOrderDeptInstance(wo)
	if dept == "" {
		dept = "engineering"
	}
	intent := strings.TrimSpace(wo.Intent)
	if intent == "" && len(wo.Instructions) > 0 {
		intent = wo.Instructions[0]
	}
	paths := EditScopePaths(wo)
	if len(paths) == 0 {
		paths = CollectEditedPaths(hist, "")
	}
	tools := lessons.ToolCountsFromHistory(hist)

	kind := lessons.KindPattern
	verify := "passed"
	fix := ""

	verifyFailSummary, verifyFailed := workerVerificationFailure(taskResult)

	switch {
	case status != "done":
		kind = lessons.KindAntiPattern
		verify = strings.TrimSpace(status)
		if verify == "" {
			verify = "error"
		}
		if errMsg := strings.TrimSpace(taskResult); errMsg != "" {
			verify = clipLessonVerify(verify, errMsg)
		}
	case !workerTaskResultSuccess(taskResult):
		kind = lessons.KindAntiPattern
		if verifyFailed {
			verify = verifyFailSummary
			fix = "retry with verification hints or escalate tier"
		} else if errMsg := extractWorkerError(taskResult); errMsg != "" {
			verify = errMsg
		} else {
			verify = "task_result not success"
		}
	case workerResultEscalated(taskResult):
		kind = lessons.KindEscalation
		verify = "passed after tier escalation"
	case verifyFailed:
		kind = lessons.KindAntiPattern
		verify = verifyFailSummary
		fix = "retry with verification hints or escalate tier"
	}

	_ = lessons.Append(projectRoot, lessons.Entry{
		Dept:   dept,
		Kind:   kind,
		Task:   intent,
		Files:  paths,
		Tools:  tools,
		Verify: verify,
		Fix:    fix,
	})
	if kind != lessons.KindAntiPattern {
		return ""
	}
	key := lessons.AntiPatternKey(verify, intent)
	count := lessons.BumpAntiPatternSignal(projectRoot, dept, key)
	if count >= lessons.PromoteSuggestThreshold {
		return lessons.FormatPromoteHint(dept, count)
	}
	return ""
}

func extractWorkerError(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	st, _ := ParseWorkerTaskResult(raw)
	if st != "" && st != "success" && st != "ok" && st != "done" {
		return st
	}
	return ""
}

func workerResultEscalated(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || !json.Valid([]byte(raw)) {
		return false
	}
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return false
	}
	tier, _ := m["escalated_to_tier"].(string)
	return strings.TrimSpace(tier) != ""
}

func clipLessonVerify(primary, extra string) string {
	extra = strings.TrimSpace(extra)
	if extra == "" {
		return primary
	}
	if primary == "" {
		return extra
	}
	return primary + ": " + extra
}

// loadDeptLessons injects recent episodic lessons for worker children.
func loadDeptLessons(root string, mode agent.Mode, wo *WorkOrder) string {
	if mode != agent.ModeWorker || wo == nil {
		return ""
	}
	dept := workOrderDeptInstance(wo)
	if dept == "" {
		dept = "engineering"
	}
	return lessons.FormatInject(root, dept)
}

// loadDeptPlaybook injects L2 playbook + local overlay for worker children.
func loadDeptPlaybook(root string, mode agent.Mode, wo *WorkOrder) string {
	if mode != agent.ModeWorker || wo == nil {
		return ""
	}
	dept := workOrderDeptInstance(wo)
	if dept == "" {
		dept = "engineering"
	}
	return playbooks.FormatDeptPlaybookInject(root, dept)
}
