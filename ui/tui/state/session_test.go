package state_test

import (
	"testing"

	"github.com/orchestra/orchestra/ui/tui/state"
)

func TestSession_AppendMessage(t *testing.T) {
	var s state.Session
	s.AppendMessage(state.Message{Role: state.RoleUser, Text: "hi"})
	s.AppendMessage(state.Message{Role: state.RoleAssistant, Text: "hello"})

	if len(s.Messages) != 2 {
		t.Fatalf("want 2 messages, got %d", len(s.Messages))
	}
	if s.Messages[0].Role != state.RoleUser || s.Messages[1].Role != state.RoleAssistant {
		t.Errorf("roles in wrong order: %+v", s.Messages)
	}
}

func TestSession_AppendMessageOrder(t *testing.T) {
	var s state.Session
	texts := []string{"first", "second", "third"}
	for _, txt := range texts {
		s.AppendMessage(state.Message{Role: state.RoleUser, Text: txt})
	}

	if len(s.Messages) != 3 {
		t.Fatalf("want 3 messages, got %d", len(s.Messages))
	}
	for i, txt := range texts {
		if s.Messages[i].Text != txt {
			t.Errorf("messages[%d].Text = %q, want %q", i, s.Messages[i].Text, txt)
		}
	}
}

func TestSession_ZeroValue(t *testing.T) {
	var s state.Session
	if s.Messages != nil {
		t.Errorf("zero-value Session should have nil Messages, got %v", s.Messages)
	}
}
