package agent

const planApprovedQuery = "The plan has been approved. Execute the plan."

func mergeAgentResults(first, second *Result) *Result {
	if second == nil {
		if first != nil {
			first.SwitchToMode = ""
		}
		return first
	}
	if first == nil {
		second.SwitchToMode = ""
		return second
	}
	merged := *first
	merged.Steps = first.Steps + second.Steps
	merged.SwitchToMode = ""
	merged.MaxStepsExceeded = second.MaxStepsExceeded
	if len(second.Patches) > 0 {
		merged.Patches = second.Patches
	}
	if len(second.Ops) > 0 {
		merged.Ops = second.Ops
	}
	if second.ApplyResponse != nil {
		merged.ApplyResponse = second.ApplyResponse
	}
	merged.Applied = second.Applied || first.Applied
	// Either run may have rewritten the shared history array; the caller
	// persists whatever the LAST one returned, so the flag has to survive a
	// rewrite in either half. OR, never "second wins".
	merged.HistoryRewritten = first.HistoryRewritten || second.HistoryRewritten
	if len(second.Todos) > 0 {
		merged.Todos = second.Todos
	} else {
		merged.Todos = first.Todos
	}
	if second.SubtaskResult != "" {
		merged.SubtaskResult = second.SubtaskResult
	}
	return &merged
}
