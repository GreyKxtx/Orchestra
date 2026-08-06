// Package state holds local session state for the TUI.
package state

import (
	"strings"
	"time"
)

// Role identifies who produced a message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

// SystemKind classifies standalone system messages and inline assistant notices.
type SystemKind string

const (
	SystemKindInfo    SystemKind = "info"
	SystemKindRetry   SystemKind = "retry"
	SystemKindError   SystemKind = "error"
	SystemKindSuccess SystemKind = "success"
)

// SystemNotice is a compact status line shown inside an assistant turn.
type SystemNotice struct {
	Kind SystemKind
	Text string
}

// Message is one entry in the chat scroll. Either Text-only (user/system)
// or Assistant with optional ToolBlocks interleaved.
type Message struct {
	Role       Role
	Text       string
	ToolBlocks []ToolBlock
	Streaming  bool // true while the assistant message is still being received

	// Reasoning holds the chain-of-thought text streamed before the answer
	// (qwen3 / deepseek-r1 / openai o1 style). Rendered as a muted block above
	// the answer when non-empty.
	Reasoning string

	// StartedAt is the moment a streaming assistant message began; Duration
	// is filled in at FinishAssistant time. Both are zero on user/system
	// messages and on assistant messages loaded from disk.
	StartedAt time.Time
	Duration  time.Duration

	// TokensIn / TokensOut come from the provider's usage report for the turn
	// (often summed across LLM steps). Used in the assistant footer.
	TokensIn  int
	TokensOut int

	// PromptCtx is the last per-step prompt size (context window fill) for the
	// status-bar tokens indicator. Distinct from TokensIn turn totals.
	PromptCtx int

	// Mode / Model identify the agent mode and LLM used at the time this
	// message was sent (user) or generated (assistant). Stored per-message
	// so the ┃ accent color and the per-turn footer reflect the historical
	// state, not the current chat config — switching modes after the fact
	// must not retroactively recolor old turns.
	Mode  string
	Model string

	// DiffFiles holds structured before/after for RoleDiff messages.
	DiffFiles    []DiffFile
	DiffExpanded bool

	// SystemKind applies to RoleSystem messages (error, retry, committed, …).
	SystemKind SystemKind

	// Notices are inline retry/status lines attached to an assistant turn.
	Notices []SystemNotice

	// ToolsExpanded persists Ctrl+T tool-group expand for reopen.
	ToolsExpanded bool
	// ReasoningExpanded persists CoT expand (default collapsed when long).
	ReasoningExpanded bool
}

// DiffFile is one file change shown inline in the chat diff panel.
type DiffFile struct {
	Path   string
	Before string
	After  string
}

// Session is the TUI's local view of the current chat.
type Session struct {
	Messages []Message

	// activeAssistant is the index into Messages of the in-flight assistant
	// message (the one currently receiving streaming deltas), or -1.
	activeAssistant int
}

// NewSession returns a session with no active assistant message.
func NewSession() *Session {
	return &Session{activeAssistant: -1}
}

// AppendMessage adds a message to history.
func (s *Session) AppendMessage(m Message) {
	s.Messages = append(s.Messages, m)
}

// AppendAssistantNotice adds a deduplicated notice to the active assistant turn.
// Falls back to a standalone system message when no assistant is streaming.
func (s *Session) AppendAssistantNotice(kind SystemKind, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if s.activeAssistant < 0 || s.activeAssistant >= len(s.Messages) {
		s.AppendMessage(Message{Role: RoleSystem, SystemKind: kind, Text: text})
		return
	}
	m := &s.Messages[s.activeAssistant]
	for _, n := range m.Notices {
		if n.Kind == kind && n.Text == text {
			return
		}
	}
	m.Notices = append(m.Notices, SystemNotice{Kind: kind, Text: text})
}

// AppendSystemNotice adds a standalone styled system message.
func (s *Session) AppendSystemNotice(kind SystemKind, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if n := len(s.Messages); n > 0 {
		prev := s.Messages[n-1]
		if prev.Role == RoleSystem && prev.SystemKind == kind && prev.Text == text {
			return
		}
	}
	s.AppendMessage(Message{Role: RoleSystem, SystemKind: kind, Text: text})
}

// StartAssistant begins a new streaming assistant message and returns its
// index. mode/model are captured into the message so the per-turn footer
// (▣ <mode> · <model>) survives subsequent mode switches.
func (s *Session) StartAssistant(mode, model string) int {
	s.Messages = append(s.Messages, Message{
		Role:      RoleAssistant,
		Streaming: true,
		StartedAt: time.Now(),
		Mode:      mode,
		Model:     model,
	})
	s.activeAssistant = len(s.Messages) - 1
	return s.activeAssistant
}

// AppendAssistantReasoningDelta appends a CoT chunk to the active assistant.
// No-op if there's no active assistant.
func (s *Session) AppendAssistantReasoningDelta(delta string) {
	if s.activeAssistant < 0 || s.activeAssistant >= len(s.Messages) {
		return
	}
	s.Messages[s.activeAssistant].Reasoning += delta
}

// SetAssistantUsage records token counts on the active assistant message.
// Called when the LLM stream ends with a usage report.
func (s *Session) SetAssistantUsage(in, out int) {
	if s.activeAssistant < 0 || s.activeAssistant >= len(s.Messages) {
		return
	}
	s.Messages[s.activeAssistant].TokensIn = in
	s.Messages[s.activeAssistant].TokensOut = out
}

// SetAssistantPromptCtx records the latest per-step prompt size for the ctx bar.
func (s *Session) SetAssistantPromptCtx(n int) {
	if n <= 0 || s.activeAssistant < 0 || s.activeAssistant >= len(s.Messages) {
		return
	}
	s.Messages[s.activeAssistant].PromptCtx = n
}

// AppendAssistantDelta appends a token to the active assistant message.
// No-op if there's no active assistant.
func (s *Session) AppendAssistantDelta(delta string) {
	if s.activeAssistant < 0 || s.activeAssistant >= len(s.Messages) {
		return
	}
	s.Messages[s.activeAssistant].Text += delta
}

// TruncateAssistantText truncates the active assistant's Text to n bytes.
// Used to discard pre-tool-call chatter when a step ends with a tool call.
func (s *Session) TruncateAssistantText(n int) {
	if s.activeAssistant < 0 || s.activeAssistant >= len(s.Messages) {
		return
	}
	m := &s.Messages[s.activeAssistant]
	if len(m.Text) > n {
		m.Text = m.Text[:n]
	}
}

// AssistantTextLen returns the current byte length of the active assistant's Text.
func (s *Session) AssistantTextLen() int {
	if s.activeAssistant < 0 || s.activeAssistant >= len(s.Messages) {
		return 0
	}
	return len(s.Messages[s.activeAssistant].Text)
}

// FindToolBlock looks up a tool block by its id in the active assistant message.
func (s *Session) FindToolBlock(id string) (ToolBlock, bool) {
	if s.activeAssistant < 0 || s.activeAssistant >= len(s.Messages) {
		return ToolBlock{}, false
	}
	for _, tb := range s.Messages[s.activeAssistant].ToolBlocks {
		if tb.ID == id {
			return tb, true
		}
	}
	return ToolBlock{}, false
}


// AppendToolBlock attaches a tool block to the active assistant message.
// If no active assistant exists, starts one with empty mode/model — caller
// should normally have called StartAssistant first so the turn's mode/model
// is captured.
func (s *Session) AppendToolBlock(tb ToolBlock) {
	if s.activeAssistant < 0 {
		s.StartAssistant("", "")
	}
	s.Messages[s.activeAssistant].ToolBlocks = append(s.Messages[s.activeAssistant].ToolBlocks, tb)
}

// AppendToolArgsDelta accumulates raw JSON-argument bytes onto the latest
// running tool block. The LLM streams arguments piece-wise across many
// tool_call_delta events, so we just append until completion.
func (s *Session) AppendToolArgsDelta(id, delta string) {
	if s.activeAssistant < 0 || s.activeAssistant >= len(s.Messages) {
		return
	}
	blocks := s.Messages[s.activeAssistant].ToolBlocks
	// Exact ID match first (reverse scan so the latest running block wins).
	if id != "" {
		for i := len(blocks) - 1; i >= 0; i-- {
			if blocks[i].Status == ToolBlockRunning && blocks[i].ID == id {
				blocks[i].ArgsRaw += delta
				return
			}
		}
	}
	// Fallback: first running block — same strategy as UpdateToolBlock.
	// Handles LLMs that omit tool IDs on streaming deltas.
	for i := range blocks {
		if blocks[i].Status == ToolBlockRunning {
			blocks[i].ArgsRaw += delta
			return
		}
	}
}

// UpdateToolBlock finds the tool block by ID in the active assistant message
// and updates its status / result. Returns true if found.
//
// Some agents (notably the local one when the LLM doesn't supply tool IDs)
// synthesize a fresh ID at completion time that doesn't match the empty ID
// emitted on tool_call_start. As a fallback, if no exact match is found we
// promote the FIRST still-running block to completed — the order of starts
// and completions is the same, so this stays correct under sequential tool
// execution.
func (s *Session) UpdateToolBlock(id string, status ToolBlockStatus, result string, diags []ToolDiagnostic) bool {
	if s.activeAssistant < 0 || s.activeAssistant >= len(s.Messages) {
		return false
	}
	blocks := s.Messages[s.activeAssistant].ToolBlocks
	idx := -1
	for i := range blocks {
		if blocks[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		for i := range blocks {
			if blocks[i].Status == ToolBlockRunning {
				idx = i
				break
			}
		}
	}
	if idx < 0 {
		return false
	}
	blocks[idx].Status = status
	blocks[idx].Result = result
	blocks[idx].Diagnostics = diags
	return true
}

// AppendRunningToolOutput appends streamed stdout to the last running exec/bash
// tool block in the active assistant message.
func (s *Session) AppendRunningToolOutput(chunk string) {
	if chunk == "" || s.activeAssistant < 0 || s.activeAssistant >= len(s.Messages) {
		return
	}
	blocks := s.Messages[s.activeAssistant].ToolBlocks
	for i := len(blocks) - 1; i >= 0; i-- {
		if blocks[i].Status != ToolBlockRunning {
			continue
		}
		name := strings.ToLower(blocks[i].Name)
		if name == "exec.run" || name == "bash" {
			blocks[i].Result += chunk
			return
		}
	}
}

// FinishAssistant marks the active assistant message as no longer streaming
// and stamps Duration if StartedAt is set.
func (s *Session) FinishAssistant() {
	if s.activeAssistant >= 0 && s.activeAssistant < len(s.Messages) {
		m := &s.Messages[s.activeAssistant]
		m.Streaming = false
		if !m.StartedAt.IsZero() {
			m.Duration = time.Since(m.StartedAt)
		}
	}
	s.activeAssistant = -1
}

const RoleDiff Role = "diff"

// AddDiffFiles appends a collapsible diff message (rendered in view layer).
func (s *Session) AddDiffFiles(files []DiffFile) {
	if len(files) == 0 {
		return
	}
	cp := append([]DiffFile(nil), files...)
	s.Messages = append(s.Messages, Message{Role: RoleDiff, DiffFiles: cp})
}

// ToggleLastDiff expands/collapses the most recent diff message.
func (s *Session) ToggleLastDiff() bool {
	for i := len(s.Messages) - 1; i >= 0; i-- {
		if s.Messages[i].Role == RoleDiff && len(s.Messages[i].DiffFiles) > 0 {
			s.Messages[i].DiffExpanded = !s.Messages[i].DiffExpanded
			return true
		}
	}
	return false
}

// RemoveDiff removes the last diff message from history.
func (s *Session) RemoveDiff() {
	for i := len(s.Messages) - 1; i >= 0; i-- {
		if s.Messages[i].Role == RoleDiff {
			s.Messages = append(s.Messages[:i], s.Messages[i+1:]...)
			return
		}
	}
}

// Clear removes all messages and resets the active assistant.
func (s *Session) Clear() {
	s.Messages = nil
	s.activeAssistant = -1
}

// SetMessages replaces the message list wholesale, used when loading a saved
// session from disk. Any active streaming assistant is reset.
func (s *Session) SetMessages(msgs []Message) {
	// Defensive copy so callers can't mutate our slice afterwards.
	if len(msgs) == 0 {
		s.Messages = nil
	} else {
		s.Messages = append([]Message(nil), msgs...)
		// Loaded messages should not be marked as streaming.
		for i := range s.Messages {
			s.Messages[i].Streaming = false
		}
	}
	s.activeAssistant = -1
}

// HasDiff reports whether there is a diff message in history.
func (s *Session) HasDiff() bool {
	for _, m := range s.Messages {
		if m.Role == RoleDiff {
			return true
		}
	}
	return false
}

// HasRunningTool reports whether the active assistant message has any tool
// block in Running status. Used by the TUI to decide whether the per-tick
// spinner repaint should happen.
func (s *Session) HasRunningTool() bool {
	if s.activeAssistant < 0 || s.activeAssistant >= len(s.Messages) {
		return false
	}
	for _, tb := range s.Messages[s.activeAssistant].ToolBlocks {
		if tb.Status == ToolBlockRunning {
			return true
		}
	}
	return false
}
