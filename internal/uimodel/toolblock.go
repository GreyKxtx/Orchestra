package uimodel

import "time"

// ToolBlockStatus describes the lifecycle stage of a tool call.
type ToolBlockStatus string

const (
	ToolBlockRunning   ToolBlockStatus = "running"
	ToolBlockCompleted ToolBlockStatus = "completed"
	ToolBlockFailed    ToolBlockStatus = "failed"
	ToolBlockSkipped   ToolBlockStatus = "skipped"
)

// ToolBlock represents one tool call inside an assistant message.
type ToolBlock struct {
	ID          string
	Name        string
	ArgsPreview string
	ArgsRaw     string
	Status      ToolBlockStatus
	Result      string
	Diagnostics []ToolDiagnostic
	Expanded    bool
	StartedAt   time.Time
	Duration    time.Duration
}

// ToolDiagnostic is one LSP diagnostic attached to a write/edit tool result.
type ToolDiagnostic struct {
	StartLine int    `json:"start_line"`
	StartCol  int    `json:"start_col"`
	EndLine   int    `json:"end_line,omitempty"`
	EndCol    int    `json:"end_col,omitempty"`
	Severity  string `json:"severity"`
	Source    string `json:"source,omitempty"`
	Message   string `json:"message"`
}
