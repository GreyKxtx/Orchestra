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

	EventPermissionRequest EventKind = "permission_request" // server asks for exec.run consent

	// Workflow stage events (Protocol v4+).
	EventWorkflowStageStart EventKind = "workflow.stage_start"
	EventWorkflowStageDone  EventKind = "workflow.stage_done"
)

// WorkflowStagePayload carries the data for workflow.stage_start / .stage_done.
type WorkflowStagePayload struct {
	Name     string `json:"name"`
	StageID  string `json:"stage_id"`
	Attempt  int    `json:"attempt"`
	Marker   string `json:"marker,omitempty"`
	Action   string `json:"action,omitempty"`
	OutputKB int    `json:"output_kb,omitempty"`
}

// Event is a TUI-side representation of a streaming event.
type Event struct {
	Kind         EventKind
	Step         int
	Content      string
	ToolCallID   string
	ToolCallName string
	ArgsDelta    string                    // only set on tool_call_delta — partial JSON of arguments
	PendingOps   *PendingOpsPayload        // only set when Kind == EventPendingOps
	PermReq      *PermissionRequestPayload // only set when Kind == EventPermissionRequest
	Stage        *WorkflowStagePayload     // only set when Kind == EventWorkflowStageStart / EventWorkflowStageDone
	Err          string                    // only set on connection/agent error events
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

// PermissionRequestPayload carries the exec.run consent request.
type PermissionRequestPayload struct {
	Tool        string `json:"tool"`
	Description string `json:"description"`
}
