/**
 * Package agent содержит определения типов для работы с агентами.
 * Включает типы шагов выполнения, вызовы инструментов и финальные патчи.
 */
package agent

import (
	"encoding/json"

	"github.com/orchestra/orchestra/patch/patches"
)

type StepType string

const (
	StepToolCall StepType = "tool_call"
	StepFinal    StepType = "final"
)

type Step struct {
	Type StepType `json:"type"`
	// Tool holds the single call for the legacy serial path. Set when the
	// response carries exactly one tool_call OR when at least one of several
	// parallel calls would mutate state (parallel batching is read-only).
	Tool *ToolCall `json:"tool,omitempty"`
	// Tools holds the parallel batch. Set when the model emits ≥2 tool_calls
	// AND every one of them is ParallelSafe (pure read). The agent fans these
	// out concurrently via a worker pool; results are stitched back into
	// history in their original order.
	Tools []ToolCall `json:"tools,omitempty"`
	Final *Final     `json:"final,omitempty"`
}

type ToolCall struct {
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type Final struct {
	Patches []patches.Patch `json:"patches"`
}
