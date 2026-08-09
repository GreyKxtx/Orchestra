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
// or Assistant with chronological Segments (reasoning / tools / text).
// Flat Text/Reasoning/ToolBlocks are projections synced from Segments for
// chrome, copy, and session-file compat.
type Message struct {
	Role       Role
	Text       string      // projection: concatenated SegmentText
	Attachments []Attachment
	ToolBlocks []ToolBlock // projection: flattened SegmentTools
	Streaming  bool        // true while the assistant message is still being received

	// Segments is the chronological SoT for assistant turns (variant A).
	Segments []Segment

	// Reasoning is the projection of SegmentReasoning bodies.
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

	// Notices is a projection of SegmentNotice entries (kept for older
	// persist readers and chrome). Chronological SoT is Segments.
	Notices []SystemNotice

	// ToolsExpanded persists Ctrl+T tool-group expand for reopen.
	ToolsExpanded bool
	// ReasoningExpanded persists CoT expand (default collapsed when long).
	ReasoningExpanded bool
}

// Attachment is a chat file/image staged for a user turn.
type Attachment struct {
	Path string
	Name string
	Kind string // image | file
	MIME string
	Ext  string
}

// DiffFile is one file change shown inline in the chat diff panel.
type DiffFile struct {
	Path         string
	Before       string
	After        string
	ReviewStatus string // "", "accepted", "rejected"
}

// Diff review status values persisted in session UI projection.
const (
	DiffReviewAccepted = "accepted"
	DiffReviewRejected = "rejected"
)

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
	m.NormalizeSegments()
	s.Messages = append(s.Messages, m)
}

// AppendAssistantNotice inserts an inline notice at the current chronological
// position in the active assistant turn (as SegmentNotice). Falls back to a
// standalone system message when no assistant is streaming.
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
	// Dedup only against other notice segments (same kind+text already shown).
	for _, seg := range m.Segments {
		if seg.Kind == SegmentNotice && seg.NoticeKind == kind && strings.TrimSpace(seg.Text) == text {
			return
		}
	}
	// Always a new segment — never coalesce consecutive notices into one block,
	// so each event keeps its place between tools/text.
	m.Segments = append(m.Segments, Segment{
		Kind:       SegmentNotice,
		Text:       text,
		NoticeKind: kind,
	})
	m.syncProjections()
}

// UpsertTodosChecklist puts/refreshes a SegmentTodos on the active streaming
// assistant turn (Claude Code-style checklist in the transcript). When the turn
// has already finished, updates the last assistant message instead.
func (s *Session) UpsertTodosChecklist(items []TodoItem) {
	if s == nil || len(items) == 0 {
		return
	}
	idx := s.activeAssistant
	if idx < 0 || idx >= len(s.Messages) {
		idx = -1
		for i := len(s.Messages) - 1; i >= 0; i-- {
			if s.Messages[i].Role == RoleAssistant {
				idx = i
				break
			}
		}
	}
	if idx < 0 {
		return
	}
	m := &s.Messages[idx]
	cp := append([]TodoItem(nil), items...)
	// Refresh the last todos segment in this turn, else append.
	for i := len(m.Segments) - 1; i >= 0; i-- {
		if m.Segments[i].Kind == SegmentTodos {
			m.Segments[i].Todos = cp
			m.syncProjections()
			return
		}
	}
	m.Segments = append(m.Segments, Segment{Kind: SegmentTodos, Todos: cp})
	m.syncProjections()
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
	m := &s.Messages[s.activeAssistant]
	i := m.ensureOpenSegment(SegmentReasoning)
	m.Segments[i].Text += delta
	m.syncProjections()
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
	m := &s.Messages[s.activeAssistant]
	i := m.ensureOpenSegment(SegmentText)
	m.Segments[i].Text += delta
	m.syncProjections()
}

// TruncateAssistantText is a no-op kept for call-site compatibility.
// Chronological segments keep mid-step narration; pre-tool chatter is no longer discarded.
func (s *Session) TruncateAssistantText(n int) {
	_ = s
	_ = n
}

// AssistantTextLen returns the current byte length of the active assistant's Text projection.
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
	m := &s.Messages[s.activeAssistant]
	si, ti := m.findToolBlockLoc(id)
	if si < 0 {
		return ToolBlock{}, false
	}
	return m.Segments[si].Tools[ti], true
}

// AppendToolBlock attaches a tool block to the active assistant message.
// If no active assistant exists, starts one with empty mode/model — caller
// should normally have called StartAssistant first so the turn's mode/model
// is captured.
func (s *Session) AppendToolBlock(tb ToolBlock) {
	if s.activeAssistant < 0 {
		s.StartAssistant("", "")
	}
	m := &s.Messages[s.activeAssistant]
	// Starting a fresh tool batch after text/reasoning — any still-running
	// tools in earlier segments are stale (step moved on without completion).
	if n := len(m.Segments); n > 0 && m.Segments[n-1].Kind != SegmentTools {
		finalizeRunningToolsBefore(m, n)
	}
	if tb.StartedAt.IsZero() {
		tb.StartedAt = time.Now()
	}
	i := m.ensureOpenSegment(SegmentTools)
	m.Segments[i].Tools = append(m.Segments[i].Tools, tb)
	m.syncProjections()
}

// AppendToolArgsDelta accumulates raw JSON-argument bytes onto the latest
// running tool block. The LLM streams arguments piece-wise across many
// tool_call_delta events, so we just append until completion.
func (s *Session) AppendToolArgsDelta(id, delta string) {
	if s.activeAssistant < 0 || s.activeAssistant >= len(s.Messages) {
		return
	}
	m := &s.Messages[s.activeAssistant]
	if id != "" {
		si, ti := m.findToolBlockLoc(id)
		if si >= 0 && m.Segments[si].Tools[ti].Status == ToolBlockRunning {
			m.Segments[si].Tools[ti].ArgsRaw += delta
			m.syncProjections()
			return
		}
	}
	// Fallback: last running block (prefer latest for streaming deltas).
	si, ti := m.lastRunningToolLoc()
	if si < 0 {
		si, ti = m.firstRunningToolLoc()
	}
	if si < 0 {
		return
	}
	m.Segments[si].Tools[ti].ArgsRaw += delta
	m.syncProjections()
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
	m := &s.Messages[s.activeAssistant]
	si, ti := m.findToolBlockLoc(id)
	if si < 0 {
		si, ti = m.firstRunningToolLoc()
	}
	if si < 0 {
		return false
	}
	m.Segments[si].Tools[ti].Status = status
	m.Segments[si].Tools[ti].Result = result
	m.Segments[si].Tools[ti].Diagnostics = diags
	tb := &m.Segments[si].Tools[ti]
	if status != ToolBlockRunning {
		if tb.Duration == 0 && !tb.StartedAt.IsZero() {
			tb.Duration = time.Since(tb.StartedAt)
		}
	}
	m.syncProjections()
	return true
}

// FinalizeRunningTools marks any still-running tool blocks as completed.
// Called at step boundaries when the agent loop has moved on.
func (s *Session) FinalizeRunningTools() {
	if s.activeAssistant < 0 || s.activeAssistant >= len(s.Messages) {
		return
	}
	m := &s.Messages[s.activeAssistant]
	finalizeRunningToolsBefore(m, len(m.Segments))
	m.syncProjections()
}

// finalizeRunningToolsBefore completes running tools in segments [0, segLimit).
func finalizeRunningToolsBefore(m *Message, segLimit int) {
	if m == nil {
		return
	}
	now := time.Now()
	for si := 0; si < segLimit && si < len(m.Segments); si++ {
		if m.Segments[si].Kind != SegmentTools {
			continue
		}
		for ti := range m.Segments[si].Tools {
			tb := &m.Segments[si].Tools[ti]
			if tb.Status != ToolBlockRunning {
				continue
			}
			tb.Status = ToolBlockCompleted
			if !tb.StartedAt.IsZero() {
				tb.Duration = now.Sub(tb.StartedAt)
			}
		}
	}
}

// AppendRunningToolOutput appends streamed stdout to the last running exec/bash
// tool block in the active assistant message.
func (s *Session) AppendRunningToolOutput(chunk string) {
	if chunk == "" || s.activeAssistant < 0 || s.activeAssistant >= len(s.Messages) {
		return
	}
	m := &s.Messages[s.activeAssistant]
	for si := len(m.Segments) - 1; si >= 0; si-- {
		if m.Segments[si].Kind != SegmentTools {
			continue
		}
		for ti := len(m.Segments[si].Tools) - 1; ti >= 0; ti-- {
			if m.Segments[si].Tools[ti].Status != ToolBlockRunning {
				continue
			}
			name := strings.ToLower(m.Segments[si].Tools[ti].Name)
			if name == "exec.run" || name == "bash" {
				m.Segments[si].Tools[ti].Result += chunk
				m.syncProjections()
				return
			}
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
			s.Messages[i].NormalizeSegments()
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

// LastDiffIndex returns the index of the newest RoleDiff message, or -1.
func (s *Session) LastDiffIndex() int {
	for i := len(s.Messages) - 1; i >= 0; i-- {
		if s.Messages[i].Role == RoleDiff && len(s.Messages[i].DiffFiles) > 0 {
			return i
		}
	}
	return -1
}

// LastDiffExpanded reports whether the newest diff message is expanded.
func (s *Session) LastDiffExpanded() bool {
	i := s.LastDiffIndex()
	if i < 0 {
		return false
	}
	return s.Messages[i].DiffExpanded
}

// ExpandLastDiff expands the newest diff message.
func (s *Session) ExpandLastDiff() bool {
	i := s.LastDiffIndex()
	if i < 0 {
		return false
	}
	s.Messages[i].DiffExpanded = true
	return true
}

// DiffFileCount returns the number of files in the newest diff message.
func (s *Session) DiffFileCount() int {
	i := s.LastDiffIndex()
	if i < 0 {
		return 0
	}
	return len(s.Messages[i].DiffFiles)
}

// SetDiffFileReviewStatus updates review status for one file in the last diff.
func (s *Session) SetDiffFileReviewStatus(fileIdx int, status string) bool {
	i := s.LastDiffIndex()
	if i < 0 || fileIdx < 0 || fileIdx >= len(s.Messages[i].DiffFiles) {
		return false
	}
	s.Messages[i].DiffFiles[fileIdx].ReviewStatus = status
	return true
}

// PendingDiffReviewCount counts files not yet accepted or rejected.
func (s *Session) PendingDiffReviewCount() int {
	i := s.LastDiffIndex()
	if i < 0 {
		return 0
	}
	n := 0
	for _, df := range s.Messages[i].DiffFiles {
		if df.ReviewStatus != DiffReviewAccepted && df.ReviewStatus != DiffReviewRejected {
			n++
		}
	}
	return n
}

// AcceptAllDiffFiles marks every file in the last diff as accepted.
func (s *Session) AcceptAllDiffFiles() {
	i := s.LastDiffIndex()
	if i < 0 {
		return
	}
	for j := range s.Messages[i].DiffFiles {
		if s.Messages[i].DiffFiles[j].ReviewStatus != DiffReviewRejected {
			s.Messages[i].DiffFiles[j].ReviewStatus = DiffReviewAccepted
		}
	}
}

// HasRunningTool reports whether the active assistant message has any tool
// block in Running status. Used by the TUI to decide whether the per-tick
// spinner repaint should happen.
func (s *Session) HasRunningTool() bool {
	if s.activeAssistant < 0 || s.activeAssistant >= len(s.Messages) {
		return false
	}
	si, _ := s.Messages[s.activeAssistant].firstRunningToolLoc()
	return si >= 0
}
