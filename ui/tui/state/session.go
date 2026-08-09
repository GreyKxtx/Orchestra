// Package state — see aliases.go for shared chat types (internal/uimodel).
package state

import (
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/uimodel"
)

// Session is the TUI's local view of the current chat.
type Session struct {
	Messages []Message

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
	for _, seg := range m.Segments {
		if seg.Kind == SegmentNotice && seg.NoticeKind == kind && strings.TrimSpace(seg.Text) == text {
			return
		}
	}
	m.Segments = append(m.Segments, Segment{
		Kind:       SegmentNotice,
		Text:       text,
		NoticeKind: kind,
	})
	m.SyncProjections()
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
	for i := len(m.Segments) - 1; i >= 0; i-- {
		if m.Segments[i].Kind == SegmentTodos {
			m.Segments[i].Todos = cp
			m.SyncProjections()
			return
		}
	}
	m.Segments = append(m.Segments, Segment{Kind: SegmentTodos, Todos: cp})
	m.SyncProjections()
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

// StartAssistant begins a new streaming assistant message and returns its index.
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
func (s *Session) AppendAssistantReasoningDelta(delta string) {
	if s.activeAssistant < 0 || s.activeAssistant >= len(s.Messages) {
		return
	}
	m := &s.Messages[s.activeAssistant]
	i := m.EnsureOpenSegment(SegmentReasoning)
	m.Segments[i].Text += delta
	m.SyncProjections()
}

// SetAssistantUsage records token counts on the active assistant message.
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
func (s *Session) AppendAssistantDelta(delta string) {
	if s.activeAssistant < 0 || s.activeAssistant >= len(s.Messages) {
		return
	}
	m := &s.Messages[s.activeAssistant]
	i := m.EnsureOpenSegment(SegmentText)
	m.Segments[i].Text += delta
	m.SyncProjections()
}

// TruncateAssistantText is a no-op kept for call-site compatibility.
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
	si, ti := m.FindToolBlockLoc(id)
	if si < 0 {
		return ToolBlock{}, false
	}
	return m.Segments[si].Tools[ti], true
}

// AppendToolBlock attaches a tool block to the active assistant message.
func (s *Session) AppendToolBlock(tb ToolBlock) {
	if s.activeAssistant < 0 {
		s.StartAssistant("", "")
	}
	m := &s.Messages[s.activeAssistant]
	if n := len(m.Segments); n > 0 && m.Segments[n-1].Kind != SegmentTools {
		uimodel.FinalizeRunningToolsBefore(m, n)
	}
	if tb.StartedAt.IsZero() {
		tb.StartedAt = time.Now()
	}
	i := m.EnsureOpenSegment(SegmentTools)
	m.Segments[i].Tools = append(m.Segments[i].Tools, tb)
	m.SyncProjections()
}

// AppendToolArgsDelta accumulates raw JSON-argument bytes onto the latest running tool block.
func (s *Session) AppendToolArgsDelta(id, delta string) {
	if s.activeAssistant < 0 || s.activeAssistant >= len(s.Messages) {
		return
	}
	m := &s.Messages[s.activeAssistant]
	if id != "" {
		si, ti := m.FindToolBlockLoc(id)
		if si >= 0 && m.Segments[si].Tools[ti].Status == ToolBlockRunning {
			m.Segments[si].Tools[ti].ArgsRaw += delta
			m.SyncProjections()
			return
		}
	}
	si, ti := m.LastRunningToolLoc()
	if si < 0 {
		si, ti = m.FirstRunningToolLoc()
	}
	if si < 0 {
		return
	}
	m.Segments[si].Tools[ti].ArgsRaw += delta
	m.SyncProjections()
}

// UpdateToolBlock finds the tool block by ID and updates its status / result.
func (s *Session) UpdateToolBlock(id string, status ToolBlockStatus, result string, diags []ToolDiagnostic) bool {
	if s.activeAssistant < 0 || s.activeAssistant >= len(s.Messages) {
		return false
	}
	m := &s.Messages[s.activeAssistant]
	si, ti := m.FindToolBlockLoc(id)
	if si < 0 {
		si, ti = m.FirstRunningToolLoc()
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
	m.SyncProjections()
	return true
}

// FinalizeRunningTools marks any still-running tool blocks as completed.
func (s *Session) FinalizeRunningTools() {
	if s.activeAssistant < 0 || s.activeAssistant >= len(s.Messages) {
		return
	}
	m := &s.Messages[s.activeAssistant]
	uimodel.FinalizeRunningToolsBefore(m, len(m.Segments))
	m.SyncProjections()
}

// AppendRunningToolOutput appends streamed stdout to the last running exec/bash tool block.
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
				m.SyncProjections()
				return
			}
		}
	}
}

// FinishAssistant marks the active assistant message as no longer streaming.
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

// AddDiffFiles appends a collapsible diff message.
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

// SetMessages replaces the message list wholesale.
func (s *Session) SetMessages(msgs []Message) {
	if len(msgs) == 0 {
		s.Messages = nil
	} else {
		s.Messages = append([]Message(nil), msgs...)
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

// HasRunningTool reports whether the active assistant has a running tool block.
func (s *Session) HasRunningTool() bool {
	if s.activeAssistant < 0 || s.activeAssistant >= len(s.Messages) {
		return false
	}
	si, _ := s.Messages[s.activeAssistant].FirstRunningToolLoc()
	return si >= 0
}
