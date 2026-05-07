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

func TestSession_StartAndDeltaAssistant(t *testing.T) {
	s := state.NewSession()
	s.StartAssistant()
	s.AppendAssistantDelta("hel")
	s.AppendAssistantDelta("lo")

	if len(s.Messages) != 1 {
		t.Fatalf("want 1 message, got %d", len(s.Messages))
	}
	if got := s.Messages[0].Text; got != "hello" {
		t.Errorf("want 'hello', got %q", got)
	}
	if !s.Messages[0].Streaming {
		t.Error("expected Streaming=true while active")
	}
}

func TestSession_ToolBlockUpdate(t *testing.T) {
	s := state.NewSession()
	s.StartAssistant()
	s.AppendToolBlock(state.ToolBlock{ID: "t1", Name: "read", Status: state.ToolBlockRunning})

	if !s.UpdateToolBlock("t1", state.ToolBlockCompleted, "12 lines") {
		t.Fatal("UpdateToolBlock returned false for known id")
	}

	blocks := s.Messages[0].ToolBlocks
	if len(blocks) != 1 {
		t.Fatalf("want 1 tool block, got %d", len(blocks))
	}
	if blocks[0].Status != state.ToolBlockCompleted {
		t.Errorf("want Completed, got %s", blocks[0].Status)
	}
	if blocks[0].Result != "12 lines" {
		t.Errorf("want '12 lines', got %q", blocks[0].Result)
	}
}

func TestSession_UpdateToolBlock_UnknownID(t *testing.T) {
	s := state.NewSession()
	s.StartAssistant()
	s.AppendToolBlock(state.ToolBlock{ID: "t1", Name: "read", Status: state.ToolBlockRunning})

	if s.UpdateToolBlock("nonexistent", state.ToolBlockCompleted, "x") {
		t.Error("UpdateToolBlock should return false for unknown id")
	}
}

func TestSession_FinishAssistant(t *testing.T) {
	s := state.NewSession()
	s.StartAssistant()
	s.FinishAssistant()
	if s.Messages[0].Streaming {
		t.Error("expected Streaming=false after Finish")
	}
}

func TestSession_AppendToolBlock_StartsAssistantIfNoneActive(t *testing.T) {
	s := state.NewSession()
	// No StartAssistant call.
	s.AppendToolBlock(state.ToolBlock{ID: "t1", Name: "read", Status: state.ToolBlockRunning})

	if len(s.Messages) != 1 {
		t.Fatalf("want 1 message (auto-started), got %d", len(s.Messages))
	}
	if s.Messages[0].Role != state.RoleAssistant {
		t.Errorf("want assistant role, got %s", s.Messages[0].Role)
	}
	if len(s.Messages[0].ToolBlocks) != 1 {
		t.Errorf("want 1 tool block, got %d", len(s.Messages[0].ToolBlocks))
	}
}

func TestSession_AppendDeltaWithoutActiveAssistant_NoOp(t *testing.T) {
	s := state.NewSession()
	s.AppendAssistantDelta("orphan delta")
	if len(s.Messages) != 0 {
		t.Errorf("want 0 messages (no-op), got %d", len(s.Messages))
	}
}
