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
	EventReasoningDelta    EventKind = "reasoning_delta"
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
	EventTurnUsage         EventKind = "turn_usage"          // usage totals from session.message result
	EventTurnTodos         EventKind = "turn_todos"          // todo list from session.message result
	EventTodosUpdated      EventKind = "todos_updated"       // live todo list after todowrite
	EventStepUsage         EventKind = "step_usage"          // per-LLM-step token totals during a turn
	EventModeRoute         EventKind = "mode_route"          // agent auto-router: agent→build|plan|explore

	EventPermissionRequest EventKind = "permission_request" // server asks for exec.run consent
	EventQuestionAsked     EventKind = "question_asked"     // server asks user via question/ask

	// Workflow stage events (Protocol v4+). Note the `/` separator — matches
	// LSP-style notification naming used by agent/event, exec/output_chunk, etc.
	EventWorkflowStageStart EventKind = "workflow/stage_start"
	EventWorkflowStageDone  EventKind = "workflow/stage_done"
)

// WorkflowStagePayload carries the data for workflow/stage_start / stage_done.
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
	SessionID    string // from agent/event envelope (session.message turns)
	TurnID       string // from agent/event envelope
	Content      string
	ToolCallID   string
	ToolCallName string
	ArgsDelta    string                    // only set on tool_call_delta — partial JSON of arguments
	PendingOps   *PendingOpsPayload        // only set when Kind == EventPendingOps
	PermReq      *PermissionRequestPayload // only set when Kind == EventPermissionRequest
	Questions    []QuestionItemPayload     // only set when Kind == EventQuestionAsked
	Stage        *WorkflowStagePayload     // only set when Kind == EventWorkflowStageStart / EventWorkflowStageDone
	Diagnostics  []ToolDiagnosticPayload   // LSP diagnostics on write/edit tool_call_completed
	Usage        *UsageTurnPayload         // token/cost totals for completed turn
	Todos        []TodoItem                // model checklist after turn / todowrite
	StopReason   string                    // completed | partial | max_steps (turn end)
	OpenTodos    int                       // open pending/in_progress todos at turn end
	ModeRoute    *ModeRoutePayload         // agent→effective mode
	LSPStatus    string                    // from core.health on init
	Err          string                    // only set on connection/agent error events
}

// ModeRoutePayload is emitted when mode=agent classifies the turn.
type ModeRoutePayload struct {
	From       string  `json:"from"`
	To         string  `json:"to"`
	Reason     string  `json:"reason,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
}

// UsageTurnPayload mirrors core.UsageSnapshot from session.message / agent.run.
type UsageTurnPayload struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	CostUSD          float64 `json:"cost_usd,omitempty"`
	Source           string  `json:"source,omitempty"` // "estimate" when agent-side heuristic
}

// TodoItem mirrors tools.TodoItem from session.message / todowrite.
type TodoItem struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Status  string `json:"status"`
}

// ToolDiagnosticPayload mirrors lsp.ToolDiagnostic in tool JSON responses.
type ToolDiagnosticPayload struct {
	StartLine int    `json:"start_line"`
	StartCol  int    `json:"start_col"`
	EndLine   int    `json:"end_line,omitempty"`
	EndCol    int    `json:"end_col,omitempty"`
	Severity  string `json:"severity"`
	Source    string `json:"source,omitempty"`
	Message   string `json:"message"`
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

// PermissionRequestPayload carries a consent request (shell or lsp.install).
type PermissionRequestPayload struct {
	Tool        string `json:"tool"`
	Description string `json:"description"`
	Kind        string `json:"kind,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

// QuestionItemPayload is one question in a question/ask server request.
type QuestionItemPayload struct {
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
}
