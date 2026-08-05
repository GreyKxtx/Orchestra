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
			Role:       string(m.Role),
			Text:       m.Text,
			Reasoning:  m.Reasoning,
			StartedAt:  m.StartedAt,
			TokensIn:   m.TokensIn,
			TokensOut:  m.TokensOut,
			Mode:       m.Mode,
			Model:      m.Model,
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
				}
				if tb.Duration > 0 {
					block.DurationMS = tb.Duration.Milliseconds()
				}
				ui.ToolBlocks = append(ui.ToolBlocks, block)
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
			Role:      state.Role(m.Role),
			Text:      m.Text,
			Reasoning: m.Reasoning,
			StartedAt: m.StartedAt,
			TokensIn:  m.TokensIn,
			TokensOut: m.TokensOut,
			Mode:      m.Mode,
			Model:     m.Model,
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
				}
				if tb.DurationMS > 0 {
					block.Duration = time.Duration(tb.DurationMS) * time.Millisecond
				}
				msg.ToolBlocks = append(msg.ToolBlocks, block)
			}
		}
		out = append(out, msg)
	}
	return out
}
