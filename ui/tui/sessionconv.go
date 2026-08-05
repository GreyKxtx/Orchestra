package tui

import (
	"github.com/orchestra/orchestra/internal/sessionfile"
	"github.com/orchestra/orchestra/internal/sessionstore"
	"github.com/orchestra/orchestra/ui/tui/state"
)

func uiMessagesFromState(msgs []state.Message) []sessionfile.UIMessage {
	return sessionstore.StateMessagesToUI(msgs)
}

func stateMessagesFromUI(msgs []sessionfile.UIMessage) []state.Message {
	return sessionstore.UIMessagesToState(msgs)
}
