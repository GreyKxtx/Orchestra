// Package state holds local session state for the TUI.
// In Phase 1 this is just a slice of messages for the echo demo;
// in Phase 2 it will be extended with pending ops, tool blocks, etc.
package state

// Role identifies who produced a message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

// Message is one entry in the chat scroll.
type Message struct {
	Role Role
	Text string
}

// Session is the TUI's local view of the current chat.
type Session struct {
	Messages []Message
}

// AppendMessage adds a message to the session history.
func (s *Session) AppendMessage(m Message) {
	s.Messages = append(s.Messages, m)
}
