package tasks

import (
	"encoding/json"
	"fmt"
	"strings"
)

// WorkOrder is the Lead → Worker JSON contract (docs/architecture/planner-worker.md).
type WorkOrder struct {
	TaskID             string         `json:"task_id,omitempty"`
	Tier               string         `json:"tier,omitempty"`
	TargetFile         string         `json:"target_file,omitempty"`
	TargetSymbol       string         `json:"target_symbol,omitempty"`
	Intent             string         `json:"intent,omitempty"`
	Context            map[string]any `json:"context,omitempty"`
	Instructions       []string       `json:"instructions,omitempty"`
	Constraints        []string       `json:"constraints,omitempty"`
	AcceptanceCriteria []string       `json:"acceptance_criteria,omitempty"`
}

// ValidateWorkOrder checks a parsed WorkOrder; returns error when intent is missing.
func ValidateWorkOrder(wo *WorkOrder) error {
	if wo == nil {
		return fmt.Errorf("workorder: nil")
	}
	if strings.TrimSpace(wo.Intent) == "" && len(wo.Instructions) == 0 {
		return fmt.Errorf("workorder: intent or instructions[] required")
	}
	return nil
}

// ParseWorkOrderJSON parses and validates JSON goal text for worker children.
func ParseWorkOrderJSON(raw string) (*WorkOrder, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || !json.Valid([]byte(raw)) {
		return nil, fmt.Errorf("workorder: goal must be JSON for subagent_type=worker")
	}
	var wo WorkOrder
	if err := json.Unmarshal([]byte(raw), &wo); err != nil {
		return nil, fmt.Errorf("workorder: invalid JSON: %w", err)
	}
	if err := ValidateWorkOrder(&wo); err != nil {
		return nil, err
	}
	return &wo, nil
}
