// Package rpcclient is the TUI's connection to orchestra core via JSON-RPC stdio.
package rpcclient

import "github.com/orchestra/orchestra/protocol/wire"

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
	EventTurnMemory        EventKind = "turn_memory"         // what the memory writer did, from session.message result
	EventRuleSuggestion    EventKind = "rule_suggestion"     // repeated anti-pattern on one file — offer an ORCHESTRA.md rule
	EventTodosUpdated      EventKind = "todos_updated"       // live todo list after todowrite
	EventStepUsage         EventKind = "step_usage"          // per-LLM-step token totals during a turn
	EventContextEstimate   EventKind = "context_estimate"    // estimated prompt-context size, never a provider measurement
	EventModeRoute         EventKind = "mode_route"          // agent auto-router: agent→build|plan|explore

	EventPermissionRequest EventKind = "permission_request" // server asks for exec.run consent
	EventQuestionAsked     EventKind = "question_asked"     // server asks user via question/ask

	// Workflow stage events (Protocol v4+). Note the `/` separator — matches
	// LSP-style notification naming used by agent/event, exec/output_chunk, etc.
	EventWorkflowStageStart EventKind = "workflow/stage_start"
	EventWorkflowStageDone  EventKind = "workflow/stage_done"

	EventChildDone    EventKind = "child_done"    // subagent finished; may carry learning promote hints
	EventChildStarted EventKind = "child_started" // subagent goroutine started
	EventChildQueued  EventKind = "child_queued"  // waiting on overlapping target_files
)

// WorkflowStagePayload carries the data for workflow/stage_start / stage_done.
type WorkflowStagePayload = wire.WorkflowStage

// Event is a TUI-side representation of a streaming event.
type Event struct {
	Kind                      EventKind
	ReqID                     int64 // correlation id for permission/question server requests
	Step                      int
	SessionID                 string // from agent/event envelope (session.message turns)
	TurnID                    string // from agent/event envelope
	Content                   string
	ToolCallID                string
	ToolCallName              string
	ArgsDelta                 string                    // only set on tool_call_delta — partial JSON of arguments
	PendingOps                *PendingOpsPayload        // only set when Kind == EventPendingOps
	PermReq                   *PermissionRequestPayload // only set when Kind == EventPermissionRequest
	Questions                 []QuestionItemPayload     // only set when Kind == EventQuestionAsked
	Stage                     *WorkflowStagePayload     // only set when Kind == EventWorkflowStageStart / EventWorkflowStageDone
	Diagnostics               []ToolDiagnosticPayload   // LSP diagnostics on write/edit tool_call_completed
	Usage                     *UsageTurnPayload         // token/cost totals for completed turn
	Todos                     []TodoItem                // model checklist after turn / todowrite
	Memory                    *MemoryNotePayload        // only set when Kind == EventTurnMemory
	RuleSuggestion            *RuleSuggestion           // only set when Kind == EventRuleSuggestion
	StopReason                string                    // completed | partial | max_steps (turn end)
	OpenTodos                 int                       // open pending/in_progress todos at turn end
	ModeRoute                 *ModeRoutePayload         // agent→effective mode
	LSPStatus                 string                    // from core.health on init
	TaskID                    string                    // child_* lifecycle
	SubagentType              string                    // child_* lifecycle
	ChildStatus               string                    // child_done status field
	Scope                     string                    // "child" tags worker tool/stream events
	WaitingFor                []string                  // child_queued: blocking task ids
	WaitingReason             string                    // child_queued reason
	LessonPromoteSuggestion   string                    // child_done learning hint
	PlaybookPromoteSuggestion string                    // child_done learning hint
	Err                       string                    // only set on connection/agent error events
}

// The payloads below are the wire's (protocol/wire): the TUI used to
// declare its own copies of each, by hand, beside the VS Code extension's
// and the web's.

// ModeRoutePayload is emitted when mode=agent classifies the turn.
type ModeRoutePayload = wire.ModeRoute

// UsageTurnPayload is token spend: a step's, an estimate's, or the turn's.
type UsageTurnPayload = wire.Usage

// MemoryNotePayload says what the end-of-turn memory writer did.
type MemoryNotePayload = wire.MemoryNote

// RuleSuggestion is a human-facing offer to turn a repeated anti-pattern
// into a project instruction.
type RuleSuggestion = wire.RuleSuggestion

// TodoItem is one row of the model's checklist.
type TodoItem = wire.TodoItem

// ToolDiagnosticPayload is one LSP diagnostic in a tool's answer.
type ToolDiagnosticPayload = wire.ToolDiagnostic

// PendingOpsPayload is the data of the pending_ops event.
type PendingOpsPayload = wire.PendingOps

// FileDiff is a file's content before and after a turn's edits.
type FileDiff = wire.FileDiff

// PermissionRequestPayload carries a consent request (shell or lsp.install).
type PermissionRequestPayload struct {
	Tool        string `json:"tool"`
	Description string `json:"description"`
	Kind        string `json:"kind,omitempty"`
	Reason      string `json:"reason,omitempty"`
	ReqID       int64  `json:"-"` // local correlation id (not part of the wire payload)
}

// QuestionItemPayload is one question in a question/ask server request.
type QuestionItemPayload = wire.QuestionItem
