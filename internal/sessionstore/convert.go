package sessionstore

import (
	"time"

	"github.com/orchestra/orchestra/internal/sessionfile"
	"github.com/orchestra/orchestra/ui/tui/state"
)

// StateMessagesToUI converts TUI messages to the persisted projection (v3+ segments).
func StateMessagesToUI(msgs []state.Message) []sessionfile.UIMessage {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]sessionfile.UIMessage, 0, len(msgs))
	for _, m := range msgs {
		m.NormalizeSegments()
		ui := sessionfile.UIMessage{
			Role:              string(m.Role),
			Text:              m.Text,
			Reasoning:         m.Reasoning,
			SystemKind:        string(m.SystemKind),
			StartedAt:         m.StartedAt,
			TokensIn:          m.TokensIn,
			TokensOut:         m.TokensOut,
			PromptCtx:         m.PromptCtx,
			Mode:              m.Mode,
			Model:             m.Model,
			ToolsExpanded:     m.ToolsExpanded,
			ReasoningExpanded: m.ReasoningExpanded,
		}
		if len(m.Attachments) > 0 {
			ui.Attachments = attachmentsToUI(m.Attachments)
		}
		if m.Duration > 0 {
			ui.DurationMS = m.Duration.Milliseconds()
		}
		if len(m.ToolBlocks) > 0 {
			ui.ToolBlocks = toolBlocksToUI(m.ToolBlocks)
		}
		if len(m.Segments) > 0 {
			ui.Segments = make([]sessionfile.UISegment, 0, len(m.Segments))
			for _, seg := range m.Segments {
				ui.Segments = append(ui.Segments, sessionfile.UISegment{
					Kind:       string(seg.Kind),
					Text:       seg.Text,
					Tools:      toolBlocksToUI(seg.Tools),
					NoticeKind: string(seg.NoticeKind),
				})
			}
		}
		if len(m.DiffFiles) > 0 {
			ui.DiffFiles = make([]sessionfile.UIDiffFile, 0, len(m.DiffFiles))
			for _, df := range m.DiffFiles {
				ui.DiffFiles = append(ui.DiffFiles, sessionfile.UIDiffFile{
					Path:         df.Path,
					Before:       df.Before,
					After:        df.After,
					ReviewStatus: df.ReviewStatus,
				})
			}
			ui.DiffExpanded = m.DiffExpanded
		}
		if len(m.Notices) > 0 {
			ui.Notices = make([]sessionfile.UINotice, 0, len(m.Notices))
			for _, n := range m.Notices {
				ui.Notices = append(ui.Notices, sessionfile.UINotice{
					Kind: string(n.Kind),
					Text: n.Text,
				})
			}
		}
		out = append(out, ui)
	}
	return out
}

// UIMessagesToState converts persisted UI messages back into TUI state.
func UIMessagesToState(msgs []sessionfile.UIMessage) []state.Message {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]state.Message, 0, len(msgs))
	for _, m := range msgs {
		msg := state.Message{
			Role:              state.Role(m.Role),
			Text:              m.Text,
			Reasoning:         m.Reasoning,
			SystemKind:        state.SystemKind(m.SystemKind),
			StartedAt:         m.StartedAt,
			TokensIn:          m.TokensIn,
			TokensOut:         m.TokensOut,
			PromptCtx:         m.PromptCtx,
			Mode:              m.Mode,
			Model:             m.Model,
			ToolsExpanded:     m.ToolsExpanded,
			ReasoningExpanded: m.ReasoningExpanded,
		}
		if m.DurationMS > 0 {
			msg.Duration = time.Duration(m.DurationMS) * time.Millisecond
		}
		if len(m.ToolBlocks) > 0 {
			msg.ToolBlocks = toolBlocksFromUI(m.ToolBlocks)
		}
		if len(m.Attachments) > 0 {
			msg.Attachments = attachmentsFromUI(m.Attachments)
		}
		if len(m.Segments) > 0 {
			msg.Segments = make([]state.Segment, 0, len(m.Segments))
			for _, seg := range m.Segments {
				msg.Segments = append(msg.Segments, state.Segment{
					Kind:       state.SegmentKind(seg.Kind),
					Text:       seg.Text,
					Tools:      toolBlocksFromUI(seg.Tools),
					NoticeKind: state.SystemKind(seg.NoticeKind),
				})
			}
		}
		if len(m.DiffFiles) > 0 {
			msg.DiffFiles = make([]state.DiffFile, 0, len(m.DiffFiles))
			for _, df := range m.DiffFiles {
				msg.DiffFiles = append(msg.DiffFiles, state.DiffFile{
					Path:         df.Path,
					Before:       df.Before,
					After:        df.After,
					ReviewStatus: df.ReviewStatus,
				})
			}
			msg.DiffExpanded = m.DiffExpanded
		}
		if len(m.Notices) > 0 {
			msg.Notices = make([]state.SystemNotice, 0, len(m.Notices))
			for _, n := range m.Notices {
				msg.Notices = append(msg.Notices, state.SystemNotice{
					Kind: state.SystemKind(n.Kind),
					Text: n.Text,
				})
			}
		}
		msg.NormalizeSegments()
		out = append(out, msg)
	}
	return out
}

func toolBlocksToUI(blocks []state.ToolBlock) []sessionfile.UIToolBlock {
	if len(blocks) == 0 {
		return nil
	}
	out := make([]sessionfile.UIToolBlock, 0, len(blocks))
	for _, tb := range blocks {
		block := sessionfile.UIToolBlock{
			ID:          tb.ID,
			Name:        tb.Name,
			ArgsPreview: tb.ArgsPreview,
			ArgsRaw:     tb.ArgsRaw,
			Status:      string(tb.Status),
			Result:      tb.Result,
			Expanded:    tb.Expanded,
		}
		if tb.Duration > 0 {
			block.DurationMS = tb.Duration.Milliseconds()
		}
		if len(tb.Diagnostics) > 0 {
			block.Diagnostics = make([]sessionfile.UIToolDiagnostic, len(tb.Diagnostics))
			for i, d := range tb.Diagnostics {
				block.Diagnostics[i] = sessionfile.UIToolDiagnostic{
					StartLine: d.StartLine,
					StartCol:  d.StartCol,
					EndLine:   d.EndLine,
					EndCol:    d.EndCol,
					Severity:  d.Severity,
					Source:    d.Source,
					Message:   d.Message,
				}
			}
		}
		out = append(out, block)
	}
	return out
}

func toolBlocksFromUI(blocks []sessionfile.UIToolBlock) []state.ToolBlock {
	if len(blocks) == 0 {
		return nil
	}
	out := make([]state.ToolBlock, 0, len(blocks))
	for _, tb := range blocks {
		block := state.ToolBlock{
			ID:          tb.ID,
			Name:        tb.Name,
			ArgsPreview: tb.ArgsPreview,
			ArgsRaw:     tb.ArgsRaw,
			Status:      state.ToolBlockStatus(tb.Status),
			Result:      tb.Result,
			Expanded:    tb.Expanded,
		}
		if tb.DurationMS > 0 {
			block.Duration = time.Duration(tb.DurationMS) * time.Millisecond
		}
		if len(tb.Diagnostics) > 0 {
			block.Diagnostics = make([]state.ToolDiagnostic, len(tb.Diagnostics))
			for i, d := range tb.Diagnostics {
				block.Diagnostics[i] = state.ToolDiagnostic{
					StartLine: d.StartLine,
					StartCol:  d.StartCol,
					EndLine:   d.EndLine,
					EndCol:    d.EndCol,
					Severity:  d.Severity,
					Source:    d.Source,
					Message:   d.Message,
				}
			}
		}
		out = append(out, block)
	}
	return out
}

func attachmentsToUI(atts []state.Attachment) []sessionfile.UIAttachment {
	if len(atts) == 0 {
		return nil
	}
	out := make([]sessionfile.UIAttachment, 0, len(atts))
	for _, a := range atts {
		out = append(out, sessionfile.UIAttachment{
			Path: a.Path,
			Name: a.Name,
			Kind: a.Kind,
			MIME: a.MIME,
			Ext:  a.Ext,
		})
	}
	return out
}

func attachmentsFromUI(atts []sessionfile.UIAttachment) []state.Attachment {
	if len(atts) == 0 {
		return nil
	}
	out := make([]state.Attachment, 0, len(atts))
	for _, a := range atts {
		out = append(out, state.Attachment{
			Path: a.Path,
			Name: a.Name,
			Kind: a.Kind,
			MIME: a.MIME,
			Ext:  a.Ext,
		})
	}
	return out
}
