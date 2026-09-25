package wire

import (
	"encoding/json"
	"errors"
)

// AgentEvent.Type values. The stream kinds come from the LLM stream
// (llm.StreamEventKind pins the same strings); the rest are the runtime's.
const (
	EventMessageDelta      = "message_delta"
	EventReasoningDelta    = "reasoning_delta"
	EventToolCallStart     = "tool_call_start"
	EventToolCallDelta     = "tool_call_delta"
	EventToolCallCompleted = "tool_call_completed"
	EventStepDone          = "step_done"
	EventPendingOps        = "pending_ops"
	EventRecoverableError  = "recoverable_error"
	EventDone              = "done"
	EventError             = "error"
	EventTodosUpdated      = "todos_updated"
	EventStepUsage         = "step_usage"
	EventContextEstimate   = "context_estimate"

	// EventModeRoute says which mode a mode=agent turn was routed to (Data: ModeRoute).
	EventModeRoute = "mode_route"

	// Child lifecycle (ProtocolVersion 12): a subagent started, waits, ended.
	EventChildStarted = "child_started"
	EventChildQueued  = "child_queued"
	EventChildDone    = "child_done"
	// EventAgentMessage is a message between agents of the agency.
	EventAgentMessage = "agent_message"
	// EventWorkordersRelayed says the runtime spawned a lead's batch_workorders.
	EventWorkordersRelayed = "workorders_relayed"
	// EventIntegrationVerify is the check run over a fan-out's combined edits.
	EventIntegrationVerify = "integration_verify"
)

// EventTypes lists every AgentEvent.Type the core sends.
func EventTypes() []string {
	return []string{
		EventMessageDelta, EventReasoningDelta, EventToolCallStart, EventToolCallDelta, EventToolCallCompleted,
		EventStepDone, EventPendingOps, EventRecoverableError, EventDone, EventError, EventTodosUpdated,
		EventStepUsage, EventContextEstimate, EventModeRoute,
		EventChildStarted, EventChildQueued, EventChildDone, EventAgentMessage, EventWorkordersRelayed, EventIntegrationVerify,
	}
}

// AgentEvent is the params of an agent/event notification: one thing that
// happened in a turn, as the client draws it. Type says which fields mean
// something; the rest are absent.
type AgentEvent struct {
	Step int    `json:"step"`
	Type string `json:"type"`
	// Content is the text of a delta, a tool's result, a child's goal
	// (child_started) or summary (child_done), an agent message; the JSON
	// checklist for todos_updated.
	Content string `json:"content"`
	// Data is the payload of pending_ops (PendingOps), step_usage and
	// context_estimate (Usage) and mode_route (ModeRoute). DecodeData reads
	// it on the client side.
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`

	// SessionID is set for session.message turns; TurnID for every turn.
	SessionID string `json:"session_id,omitempty"`
	TurnID    string `json:"turn_id,omitempty"`

	// Tool calls: tool_call_start carries the name and id; tool_call_delta
	// the next piece of the arguments; tool_call_completed the result in
	// Content and the diagnostics an edit produced. ToolCallIndex tells
	// parallel calls of one step apart when a model sends no ids.
	ToolCallID    string           `json:"tool_call_id,omitempty"`
	ToolCallName  string           `json:"tool_call_name,omitempty"`
	ToolCallIndex int              `json:"tool_call_index"`
	ArgsDelta     string           `json:"args_delta,omitempty"`
	Diagnostics   []ToolDiagnostic `json:"diagnostics,omitempty"`

	// Child scope: Scope is "child" on every event a subagent emits and on
	// the child_* events, with the task and its parent.
	Scope            string `json:"scope,omitempty"`
	TaskID           string `json:"task_id,omitempty"`
	ParentToolCallID string `json:"parent_tool_call_id,omitempty"`
	ParentTaskID     string `json:"parent_task_id,omitempty"`
	SubagentType     string `json:"subagent_type,omitempty"`
	// Tier and Model are the child's, on child_started.
	Tier  string `json:"tier,omitempty"`
	Model string `json:"model,omitempty"`
	// Status is the child's result status on child_done, the verdict on
	// integration_verify.
	Status string `json:"status,omitempty"`
	// Depth, Agent and ParentAgent place the child in the delegation tree.
	Depth       int    `json:"depth,omitempty"`
	Agent       string `json:"agent,omitempty"`
	ParentAgent string `json:"parent_agent,omitempty"`
	// WaitingFor and Reason say why a child_queued task waits.
	WaitingFor []string `json:"waiting_for,omitempty"`
	Reason     string   `json:"reason,omitempty"`
	// Promote suggestions a child's task_result carried (child_done).
	LessonPromoteSuggestion   string `json:"lesson_promote_suggestion,omitempty"`
	PlaybookPromoteSuggestion string `json:"playbook_promote_suggestion,omitempty"`

	// agent_message: Channel is send | reply | post.
	Channel string `json:"channel,omitempty"`
	From    string `json:"from,omitempty"`
	To      string `json:"to,omitempty"`
	Kind    string `json:"kind,omitempty"`

	// workorders_relayed: the tasks spawned and how many orders were refused.
	TaskIDs  []string `json:"task_ids,omitempty"`
	Rejected int      `json:"rejected,omitempty"`
	// integration_verify: what was checked and the one-line summary.
	Workers int    `json:"workers,omitempty"`
	Files   int    `json:"files,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// ErrNoData is DecodeData's answer to an event without a payload.
var ErrNoData = errors.New("wire: event carries no data")

// DecodeData decodes e.Data into v — a typed payload, a map, raw JSON or
// the string form older cores sent, whichever the event holds after its
// trip through the wire.
func (e AgentEvent) DecodeData(v any) error {
	switch d := e.Data.(type) {
	case nil:
		return ErrNoData
	case json.RawMessage:
		return json.Unmarshal(d, v)
	case []byte:
		return json.Unmarshal(d, v)
	case string:
		return json.Unmarshal([]byte(d), v)
	}
	b, err := json.Marshal(e.Data)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// ToolDiagnostic is one LSP diagnostic an edit produced, 1-based.
type ToolDiagnostic struct {
	StartLine int    `json:"start_line"`
	StartCol  int    `json:"start_col"`
	EndLine   int    `json:"end_line,omitempty"`
	EndCol    int    `json:"end_col,omitempty"`
	Severity  string `json:"severity"`
	Source    string `json:"source,omitempty"`
	Message   string `json:"message"`
}

// PendingOps is the Data of a pending_ops event: the internal ops a turn
// staged (or applied, when Applied), with a before/after diff per file.
// Ops are patch/ops objects (type, path, …), which the client sends back
// as they are to session.apply_pending; the protocol module does not
// decode them.
type PendingOps struct {
	Ops     []map[string]any `json:"ops"`
	Diff    []FileDiff       `json:"diff"`
	Applied bool             `json:"applied"`
}

// FileDiff is a file's content before and after a turn's edits.
type FileDiff struct {
	Path   string `json:"path"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// Usage is token spend: the Data of step_usage (one LLM call, measured by
// the provider) and context_estimate (a byte-derived estimate of the next
// prompt, Source "estimate", with its Breakdown), and the turn's total in
// agent.run and session.message results. The estimate and the measurement
// must never be summed or substituted for one another.
type Usage struct {
	Calls            int     `json:"calls,omitempty"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	CostUSD          float64 `json:"cost_usd,omitempty"`
	Source           string  `json:"source,omitempty"`
	// CachedPromptTokens is the part of PromptTokens the provider served
	// from its prompt cache; CacheWriteTokens is what it charged to fill it.
	CachedPromptTokens int `json:"cached_prompt_tokens,omitempty"`
	CacheWriteTokens   int `json:"cache_write_tokens,omitempty"`
	// Entries is the per-(provider, model) split of a turn.
	Entries []UsageEntry `json:"entries,omitempty"`
	// Breakdown is the per-category split of an estimate.
	Breakdown []ContextBreakdown `json:"breakdown,omitempty"`
}

// UsageEntry is one (provider, model) row of a turn's spend.
type UsageEntry struct {
	Provider           string  `json:"provider"`
	Model              string  `json:"model"`
	Calls              int     `json:"calls"`
	PromptTokens       int     `json:"prompt_tokens"`
	CompletionTokens   int     `json:"completion_tokens"`
	TotalTokens        int     `json:"total_tokens"`
	CostUSD            float64 `json:"cost_usd,omitempty"`
	CachedPromptTokens int     `json:"cached_prompt_tokens,omitempty"`
	CacheWriteTokens   int     `json:"cache_write_tokens,omitempty"`
}

// ContextBreakdown is one category of a prompt-context estimate.
type ContextBreakdown struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Tokens int    `json:"tokens"`
}

// ModeRoute is the Data of a mode_route event.
type ModeRoute struct {
	From       string  `json:"from"`
	To         string  `json:"to"`
	Reason     string  `json:"reason,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
}

// MemoryNote says what the end-of-turn memory writer did. Outcome is
// written | skipped | failed; Source is model | digest; Detail is the note
// when written, the reason otherwise.
type MemoryNote struct {
	Outcome string `json:"outcome"`
	Source  string `json:"source,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// RuleSuggestion offers the person a project rule for a repeated
// anti-pattern. Text is the chat-facing prompt; RuleLine is the exact line
// lesson.rule_respond appends to ORCHESTRA.md on accept.
type RuleSuggestion struct {
	Dept     string `json:"dept"`
	File     string `json:"file"`
	Count    int    `json:"count"`
	Verify   string `json:"verify,omitempty"`
	RuleLine string `json:"rule_line"`
	Text     string `json:"text"`
}

// TodoItem is one row of the model's checklist.
type TodoItem struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Status  string `json:"status"`
}

// WorkflowStage is the params of workflow/stage_start and workflow/stage_done.
type WorkflowStage struct {
	Name     string `json:"name"`
	StageID  string `json:"stage_id"`
	Attempt  int    `json:"attempt"`
	Marker   string `json:"marker,omitempty"`
	Action   string `json:"action,omitempty"`
	OutputKB int    `json:"output_kb,omitempty"`
}

// ExecOutputChunk is the params of exec/output_chunk: a piece of a running
// command's output.
type ExecOutputChunk struct {
	Step      int    `json:"step"`
	Chunk     string `json:"chunk"`
	SessionID string `json:"session_id,omitempty"`
	TurnID    string `json:"turn_id,omitempty"`
	// Child scope, as on AgentEvent, when a subagent's command is running.
	Scope            string `json:"scope,omitempty"`
	TaskID           string `json:"task_id,omitempty"`
	ParentToolCallID string `json:"parent_tool_call_id,omitempty"`
	SubagentType     string `json:"subagent_type,omitempty"`
}

// PermissionRequest is the params of permission/request. Kind is "" or
// "exec" for a shell command, "lsp.install" for a language server.
type PermissionRequest struct {
	Tool        string `json:"tool"`
	Description string `json:"description"`
	Reason      string `json:"reason,omitempty"`
	Kind        string `json:"kind,omitempty"`
}

// PermissionDecision answers permission/request. Always asks the core to
// remember the decision for the session.
type PermissionDecision struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason,omitempty"`
	Always   bool   `json:"always,omitempty"`
}

// QuestionAsk is the params of question/ask.
type QuestionAsk struct {
	Questions []QuestionItem `json:"questions"`
}

// QuestionItem is one question for the person.
type QuestionItem struct {
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
}

// QuestionAnswers answers question/ask, one answer per question.
type QuestionAnswers struct {
	Answers []string `json:"answers"`
}

// BrowserCall is the params of browser/call: a browser.* op for the
// client's own browser view. The client answers with the op's result, or
// {"error": reason} when it refuses.
type BrowserCall struct {
	Op     string         `json:"op"`
	Params map[string]any `json:"params"`
}
