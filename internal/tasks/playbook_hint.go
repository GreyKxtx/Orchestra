package tasks

import (
	"github.com/orchestra/orchestra/internal/playbooks"
)

func (r *TaskRunner) attachPlaybookPromoteHints(taskResult string, wo *WorkOrder) string {
	if r == nil || r.toolRunner == nil {
		return taskResult
	}
	root := r.toolRunner.WorkspaceRoot()
	depts := playbooks.DeptsNeedingPromoteHint(root)
	if len(depts) == 0 {
		return taskResult
	}
	dept := ""
	if wo != nil {
		dept = workOrderDeptInstance(wo)
	}
	if dept == "" || !containsDept(depts, dept) {
		dept = depts[0]
	}
	return annotatePlaybookPromoteSuggestion(taskResult, playbooks.FormatPlaybookPromoteHint(dept))
}

func containsDept(depts []string, want string) bool {
	for _, d := range depts {
		if d == want {
			return true
		}
	}
	return false
}
