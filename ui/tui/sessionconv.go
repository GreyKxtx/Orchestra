package tui

import (
	"github.com/orchestra/orchestra/internal/sessionfile"
	"github.com/orchestra/orchestra/internal/uimodel"
	"github.com/orchestra/orchestra/ui/tui/state"
)

func uiMessagesFromState(msgs []state.Message) []sessionfile.UIMessage {
	return uimodel.ToSessionfile(msgs)
}

func stateMessagesFromUI(msgs []sessionfile.UIMessage) []state.Message {
	return uimodel.FromSessionfile(msgs)
}
