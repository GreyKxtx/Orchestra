package uimodel

import (
	"strings"
	"time"
)

// SegmentKind identifies one chronological slice of an assistant turn.
type SegmentKind string

const (
	SegmentReasoning SegmentKind = "reasoning"
	SegmentText      SegmentKind = "text"
	SegmentTools     SegmentKind = "tools"
	SegmentNotice    SegmentKind = "notice"
	SegmentTodos     SegmentKind = "todos"
)

// Segment is one chronological part of an assistant message.
type Segment struct {
	Kind       SegmentKind
	Text       string
	Tools      []ToolBlock
	Todos      []TodoItem
	NoticeKind SystemKind
}

// TodoItem is one checklist row in an assistant SegmentTodos.
type TodoItem struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Status  string `json:"status"`
}

// NormalizeSegments ensures Segments is populated from legacy flat fields.
func (m *Message) NormalizeSegments() {
	if m == nil || m.Role != RoleAssistant {
		return
	}
	if len(m.Segments) == 0 {
		if strings.TrimSpace(m.Reasoning) != "" {
			m.Segments = append(m.Segments, Segment{Kind: SegmentReasoning, Text: m.Reasoning})
		}
		if len(m.ToolBlocks) > 0 {
			m.Segments = append(m.Segments, Segment{
				Kind:  SegmentTools,
				Tools: append([]ToolBlock(nil), m.ToolBlocks...),
			})
		}
		if strings.TrimSpace(m.Text) != "" {
			m.Segments = append(m.Segments, Segment{Kind: SegmentText, Text: m.Text})
		}
		for _, n := range m.Notices {
			if strings.TrimSpace(n.Text) == "" {
				continue
			}
			m.Segments = append(m.Segments, Segment{
				Kind:       SegmentNotice,
				Text:       n.Text,
				NoticeKind: n.Kind,
			})
		}
	} else if len(m.Notices) > 0 && !m.hasNoticeSegments() {
		for _, n := range m.Notices {
			if strings.TrimSpace(n.Text) == "" {
				continue
			}
			m.Segments = append(m.Segments, Segment{
				Kind:       SegmentNotice,
				Text:       n.Text,
				NoticeKind: n.Kind,
			})
		}
	}
	m.syncProjections()
}

func (m *Message) hasNoticeSegments() bool {
	for _, seg := range m.Segments {
		if seg.Kind == SegmentNotice {
			return true
		}
	}
	return false
}

// SyncProjections refreshes flat Text/Reasoning/ToolBlocks/Notices from Segments.
func (m *Message) SyncProjections() { m.syncProjections() }

func (m *Message) syncProjections() {
	if m == nil {
		return
	}
	var reasoning, text string
	var tools []ToolBlock
	var notices []SystemNotice
	for _, seg := range m.Segments {
		switch seg.Kind {
		case SegmentReasoning:
			reasoning += seg.Text
		case SegmentText:
			text += seg.Text
		case SegmentTools:
			tools = append(tools, seg.Tools...)
		case SegmentNotice:
			if t := strings.TrimSpace(seg.Text); t != "" {
				notices = append(notices, SystemNotice{Kind: seg.NoticeKind, Text: t})
			}
		}
	}
	m.Reasoning = reasoning
	m.Text = text
	m.ToolBlocks = tools
	m.Notices = notices
}

// HasVisibleContent reports whether the assistant turn has anything to show.
func (m Message) HasVisibleContent() bool {
	if len(m.Segments) > 0 {
		for _, seg := range m.Segments {
			switch seg.Kind {
			case SegmentReasoning, SegmentText, SegmentNotice:
				if strings.TrimSpace(seg.Text) != "" {
					return true
				}
			case SegmentTools:
				if len(seg.Tools) > 0 {
					return true
				}
			case SegmentTodos:
				if len(seg.Todos) > 0 {
					return true
				}
			}
		}
		return false
	}
	return strings.TrimSpace(m.Reasoning) != "" || strings.TrimSpace(m.Text) != "" || len(m.ToolBlocks) > 0 || len(m.Notices) > 0
}

// EnsureOpenSegment returns the index of the last segment if it matches kind,
// otherwise appends a new one and returns its index.
func (m *Message) EnsureOpenSegment(kind SegmentKind) int {
	if n := len(m.Segments); n > 0 && m.Segments[n-1].Kind == kind {
		return n - 1
	}
	m.Segments = append(m.Segments, Segment{Kind: kind})
	return len(m.Segments) - 1
}

// FindToolBlockLoc returns segment index and tool index for id, or (-1,-1).
func (m *Message) FindToolBlockLoc(id string) (segIdx, toolIdx int) {
	for si := range m.Segments {
		if m.Segments[si].Kind != SegmentTools {
			continue
		}
		for ti := range m.Segments[si].Tools {
			if id != "" && m.Segments[si].Tools[ti].ID == id {
				return si, ti
			}
		}
	}
	return -1, -1
}

// FirstRunningToolLoc returns the first running tool across segments.
func (m *Message) FirstRunningToolLoc() (segIdx, toolIdx int) {
	for si := range m.Segments {
		if m.Segments[si].Kind != SegmentTools {
			continue
		}
		for ti := range m.Segments[si].Tools {
			if m.Segments[si].Tools[ti].Status == ToolBlockRunning {
				return si, ti
			}
		}
	}
	return -1, -1
}

// LastRunningToolLoc returns the last running tool across segments.
func (m *Message) LastRunningToolLoc() (segIdx, toolIdx int) {
	for si := len(m.Segments) - 1; si >= 0; si-- {
		if m.Segments[si].Kind != SegmentTools {
			continue
		}
		for ti := len(m.Segments[si].Tools) - 1; ti >= 0; ti-- {
			if m.Segments[si].Tools[ti].Status == ToolBlockRunning {
				return si, ti
			}
		}
	}
	return -1, -1
}

// FinalizeRunningToolsBefore completes running tools in segments [0, segLimit).
func FinalizeRunningToolsBefore(m *Message, segLimit int) {
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
