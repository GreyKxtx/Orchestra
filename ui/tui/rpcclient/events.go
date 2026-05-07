// Package rpcclient is the TUI's connection to orchestra core via JSON-RPC stdio.
package rpcclient

// EventKind is a TUI-friendly enumeration of event types streamed from the core.
// Mirrors agent/event "type" field plus our own connection events.
type EventKind string

const (
	EventConnecting       EventKind = "connecting"
	EventInitialized      EventKind = "initialized"
	EventConnectionClosed EventKind = "connection_closed"
	EventConnectionError  EventKind = "connection_error"

	EventMessageDelta      EventKind = "message_delta"
	EventToolCallStart     EventKind = "tool_call_start"
	EventToolCallDelta     EventKind = "tool_call_delta"
	EventToolCallCompleted EventKind = "tool_call_completed"
	EventStepDone          EventKind = "step_done"
	EventPendingOps        EventKind = "pending_ops"
	EventRecoverableError  EventKind = "recoverable_error"
	EventDone              EventKind = "done"
	EventError             EventKind = "error"

	EventExecOutputChunk EventKind = "exec_output_chunk"

	EventAgentRunCompleted EventKind = "agent_run_completed" // synthesized when AgentRun returns
)

// Event is a TUI-side representation of a streaming event.
type Event struct {
	Kind         EventKind
	Step         int
	Content      string
	ToolCallID   string
	ToolCallName string
	PendingOps   *PendingOpsPayload // only set when Kind == EventPendingOps
	Err          string             // only set on connection/agent error events
}

// PendingOpsPayload mirrors the data sub-object in the pending_ops event.
type PendingOpsPayload struct {
	Ops     []map[string]any `json:"ops"`
	Diff    []FileDiff       `json:"diff"`
	Applied bool             `json:"applied"`
}

// FileDiff matches applier.FileDiff shape from the protocol.
type FileDiff struct {
	Path   string `json:"path"`
	Before string `json:"before"`
	After  string `json:"after"`
}
