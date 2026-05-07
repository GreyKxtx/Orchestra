package state

import "time"

// ToolBlockStatus describes the lifecycle stage of a tool call.
type ToolBlockStatus string

const (
	ToolBlockRunning   ToolBlockStatus = "running"
	ToolBlockCompleted ToolBlockStatus = "completed"
	ToolBlockFailed    ToolBlockStatus = "failed"
)

// ToolBlock represents one tool call inside an assistant message.
type ToolBlock struct {
	ID          string
	Name        string
	ArgsPreview string
	Status      ToolBlockStatus
	Result      string // truncated preview
	StartedAt   time.Time
	Duration    time.Duration
}
