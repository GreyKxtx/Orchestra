package sessionstore

import (
	"time"

	"github.com/orchestra/orchestra/internal/sessionfile"
	"github.com/orchestra/orchestra/ui/tui/state"
)

// StateMessagesToUI converts TUI messages to the persisted v2 projection.
func StateMessagesToUI(msgs []state.Message) []sessionfile.UIMessage {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]sessionfile.UIMessage, 0, len(msgs))
	for _, m := range msgs {
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
		if m.Duration > 0 {
			ui.DurationMS = m.Duration.Milliseconds()
		}
		if len(m.ToolBlocks) > 0 {
			ui.ToolBlocks = make([]sessionfile.UIToolBlock, 0, len(m.ToolBlocks))
			for _, tb := range m.ToolBlocks {
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
				ui.ToolBlocks = append(ui.ToolBlocks, block)
			}
		}
		if len(m.DiffFiles) > 0 {
			ui.DiffFiles = make([]sessionfile.UIDiffFile, 0, len(m.DiffFiles))
			for _, df := range m.DiffFiles {
				ui.DiffFiles = append(ui.DiffFiles, sessionfile.UIDiffFile{
					Path:   df.Path,
					Before: df.Before,
					After:  df.After,
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
			msg.ToolBlocks = make([]state.ToolBlock, 0, len(m.ToolBlocks))
			for _, tb := range m.ToolBlocks {
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
				msg.ToolBlocks = append(msg.ToolBlocks, block)
			}
		}
		if len(m.DiffFiles) > 0 {
			msg.DiffFiles = make([]state.DiffFile, 0, len(m.DiffFiles))
			for _, df := range m.DiffFiles {
				msg.DiffFiles = append(msg.DiffFiles, state.DiffFile{
					Path:   df.Path,
					Before: df.Before,
					After:  df.After,
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
		out = append(out, msg)
	}
	return out
}
