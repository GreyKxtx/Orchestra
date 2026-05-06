/**
 * Package agent содержит определения типов для работы с агентами.
 * Включает типы шагов выполнения, вызовы инструментов и финальные патчи.
 */
package agent

import (
	"encoding/json"

	"github.com/orchestra/orchestra/internal/patches"
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
	Patches []patches.Patch `json:"patches"`
}
