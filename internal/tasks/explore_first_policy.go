package tasks

import (
	"strings"
)

const exploreFirstWorkerPolicyBlock = `<explore_first_policy>
Before write or edit: call read on every WorkOrder target_file at least once (grep, glob, explore, or symbols on scope also satisfy the gate).
The runtime blocks edit/write until exploration is satisfied.
</explore_first_policy>`

func exploreFirstWorkerPolicy(wo *WorkOrder) string {
	if wo == nil {
		return exploreFirstWorkerPolicyBlock
	}
	paths := EditScopePaths(wo)
	if len(paths) == 0 {
		return exploreFirstWorkerPolicyBlock
	}
	var b strings.Builder
	b.WriteString("<explore_first_policy>\n")
	b.WriteString("Before write or edit: call read on every WorkOrder target_file at least once (grep/glob/explore on scope also satisfy the gate).\n")
	b.WriteString("target_files: ")
	b.WriteString(strings.Join(paths, ", "))
	b.WriteString("\n</explore_first_policy>")
	return b.String()
}
