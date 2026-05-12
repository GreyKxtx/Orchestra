package state

import "time"

// ToolBlockStatus describes the lifecycle stage of a tool call.
type ToolBlockStatus string

const (
	ToolBlockRunning   ToolBlockStatus = "running"
	ToolBlockCompleted ToolBlockStatus = "completed"
	ToolBlockFailed    ToolBlockStatus = "failed"
	// ToolBlockSkipped marks a tool call that was intentionally not executed —
	// usually because the model sent it as part of a mixed parallel batch
	// (read+mutating) where the agent only ran the first call to stay safe.
	// Distinct from Failed: skipping is a routing decision, not a tool error,
	// and it shouldn't be displayed in alarm-red.
	ToolBlockSkipped ToolBlockStatus = "skipped"
)

// ToolBlock represents one tool call inside an assistant message.
type ToolBlock struct {
	ID          string
	Name        string
	ArgsPreview string // human-readable per-tool summary; rendered next to the icon
	ArgsRaw     string // accumulated raw JSON arguments; available for richer block renders
	Status      ToolBlockStatus
	Result      string
	Expanded    bool // true = show full result; toggled by Tab
	StartedAt   time.Time
	Duration    time.Duration
}
