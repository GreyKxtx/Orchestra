// Package state holds local session state for the TUI.
package state

// Role identifies who produced a message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

// Message is one entry in the chat scroll. Either Text-only (user/system)
// or Assistant with optional ToolBlocks interleaved.
type Message struct {
	Role       Role
	Text       string
	ToolBlocks []ToolBlock
	Streaming  bool // true while the assistant message is still being received
}

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
	s.Messages = append(s.Messages, m)
}

// StartAssistant begins a new streaming assistant message and returns its index.
func (s *Session) StartAssistant() int {
	s.Messages = append(s.Messages, Message{Role: RoleAssistant, Streaming: true})
	s.activeAssistant = len(s.Messages) - 1
	return s.activeAssistant
}

// AppendAssistantDelta appends a token to the active assistant message.
// No-op if there's no active assistant.
func (s *Session) AppendAssistantDelta(delta string) {
	if s.activeAssistant < 0 || s.activeAssistant >= len(s.Messages) {
		return
	}
	s.Messages[s.activeAssistant].Text += delta
}

// AppendToolBlock attaches a tool block to the active assistant message.
// If no active assistant exists, starts one.
func (s *Session) AppendToolBlock(tb ToolBlock) {
	if s.activeAssistant < 0 {
		s.StartAssistant()
	}
	s.Messages[s.activeAssistant].ToolBlocks = append(s.Messages[s.activeAssistant].ToolBlocks, tb)
}

// UpdateToolBlock finds the tool block by ID in the active assistant message
// and updates its status / result. Returns true if found.
func (s *Session) UpdateToolBlock(id string, status ToolBlockStatus, result string) bool {
	if s.activeAssistant < 0 || s.activeAssistant >= len(s.Messages) {
		return false
	}
	blocks := s.Messages[s.activeAssistant].ToolBlocks
	for i := range blocks {
		if blocks[i].ID == id {
			blocks[i].Status = status
			blocks[i].Result = result
			return true
		}
	}
	return false
}

// FinishAssistant marks the active assistant message as no longer streaming.
func (s *Session) FinishAssistant() {
	if s.activeAssistant >= 0 && s.activeAssistant < len(s.Messages) {
		s.Messages[s.activeAssistant].Streaming = false
	}
	s.activeAssistant = -1
}
