package tasks

import (
	"encoding/json"
	"strings"
)

// FormatChildGoal builds the user message for a child agent.
// Workers never receive parent chat history — only a focused WorkOrder.
// If goal is already JSON, it is left as-is (Lead already emitted a WorkOrder).
// Otherwise we wrap free-form text into a minimal WorkOrder with acceptance_criteria.
func FormatChildGoal(subagentType, tier, goal string) string {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return goal
	}
	if !strings.EqualFold(strings.TrimSpace(subagentType), "worker") {
		return goal
	}
	if json.Valid([]byte(goal)) {
		// Ensure acceptance_criteria exists when Lead sent partial JSON.
		var m map[string]any
		if err := json.Unmarshal([]byte(goal), &m); err == nil {
			if _, ok := m["acceptance_criteria"]; !ok {
				m["acceptance_criteria"] = defaultWorkerCriteria()
			}
			if tier != "" {
				if _, ok := m["tier"]; !ok {
					m["tier"] = tier
				}
			}
			if b, err := json.MarshalIndent(m, "", "  "); err == nil {
				return string(b)
			}
		}
		return goal
	}
	wo := map[string]any{
		"intent":              goal,
		"instructions":        []string{goal},
		"acceptance_criteria": defaultWorkerCriteria(),
		"constraints":         []string{"One atomic change", "Prefer edit over write", "Call task_result when done"},
	}
	if t := strings.TrimSpace(tier); t != "" {
		wo["tier"] = t
	} else {
		wo["tier"] = "focused"
	}
	b, err := json.MarshalIndent(wo, "", "  ")
	if err != nil {
		return goal
	}
	return string(b)
}

func defaultWorkerCriteria() []string {
	return []string{
		"Change matches intent",
		"Search/replace is unique when using edit",
		"No unrelated files touched",
		"task_result reports success or clear error",
	}
}
