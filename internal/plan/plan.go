package plan

import (
	"encoding/json"
	"fmt"
	"strings"
)

// StepAction represents the type of action to perform on a file
type StepAction string

const (
	ActionModify StepAction = "modify" // Modify existing file
	ActionCreate StepAction = "create" // Create new file
	ActionDelete StepAction = "delete" // Delete file (not implemented in v0.2)
)

// PlanStep represents a single step in the plan
type PlanStep struct {
	FilePath string     `json:"file_path"`
	Action   StepAction `json:"action"`
	Summary  string     `json:"summary"`
}

// Plan represents the complete plan of changes
type Plan struct {
	Steps []PlanStep `json:"steps"`
}

// ParsePlan parses JSON plan from LLM response
func ParsePlan(jsonStr string) (*Plan, error) {
	// Try to extract JSON from response (might have markdown code blocks)
	jsonStr = strings.TrimSpace(jsonStr)

	// Remove markdown code blocks if present
	if strings.HasPrefix(jsonStr, "```") {
		lines := strings.Split(jsonStr, "\n")
		var jsonLines []string
		inCodeBlock := false
		for _, line := range lines {
			if strings.HasPrefix(line, "```") {
				inCodeBlock = !inCodeBlock
				continue
			}
			if inCodeBlock {
				jsonLines = append(jsonLines, line)
			} else if !strings.HasPrefix(strings.TrimSpace(line), "```") {
				jsonLines = append(jsonLines, line)
			}
		}
		jsonStr = strings.Join(jsonLines, "\n")
	}

	var p Plan
	if err := json.Unmarshal([]byte(jsonStr), &p); err != nil {
		return nil, fmt.Errorf("failed to parse plan JSON: %w\n\nRaw response:\n%s", err, jsonStr)
	}

	// Validate plan
	if len(p.Steps) == 0 {
		return nil, fmt.Errorf("plan has no steps")
	}

	// Validate actions
	for i, step := range p.Steps {
		if step.FilePath == "" {
			return nil, fmt.Errorf("step %d has empty file_path", i+1)
		}
		if step.Action != ActionModify && step.Action != ActionCreate && step.Action != ActionDelete {
			return nil, fmt.Errorf("step %d has invalid action: %s", i+1, step.Action)
		}
	}

	return &p, nil
}
