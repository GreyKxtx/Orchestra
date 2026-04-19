package agent

import (
	"encoding/json"

	"github.com/orchestra/orchestra/internal/externalpatch"
)

type StepType string

const (
	StepToolCall StepType = "tool_call"
	StepFinal    StepType = "final"
)

type Step struct {
	Type  StepType  `json:"type"`
	Tool  *ToolCall `json:"tool,omitempty"`
	Final *Final    `json:"final,omitempty"`
}

type ToolCall struct {
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type Final struct {
	Patches []externalpatch.Patch `json:"patches"`
}
