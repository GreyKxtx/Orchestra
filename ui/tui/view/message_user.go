package view

import "github.com/orchestra/orchestra/ui/tui/state"

// renderUserMessage — port of OpenCode UserMessage. Plain text in a panel-bg
// block with a thick ┃ in the message's mode color.
//
// Visual:
//
//	┃ <user text wrapped to width-2>
//
// Accent color comes from the message's own Mode (stored when the user
// sent it) — switching the current mode afterwards does NOT recolor old
// turns. Falls back to the chat's current mode when the message predates
// per-message mode tracking (loaded sessions).
func (c Chat) renderUserMessage(m state.Message, width int) string {
	mode := m.Mode
	if mode == "" {
		mode = c.chatMode
	}
	return panelBlock(m.Text, PanelOpts{
		Width:   width,
		Accent:  ModeColor(mode),
		Padding: [2]int{1, 2},
	})
}
