package state_test

import (
	"strings"
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
	s.StartAssistant("", "")
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
	s.StartAssistant("", "")
	s.AppendToolBlock(state.ToolBlock{ID: "t1", Name: "read", Status: state.ToolBlockRunning})

	if !s.UpdateToolBlock("t1", state.ToolBlockCompleted, "12 lines", nil) {
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

func TestSession_UpdateToolBlock_UnknownID_FallsBackToRunning(t *testing.T) {
	// When the agent synthesizes an ID at completion time that doesn't match
	// the empty/different ID emitted on tool_call_start, UpdateToolBlock
	// promotes the first still-running block to completed instead of failing.
	s := state.NewSession()
	s.StartAssistant("", "")
	s.AppendToolBlock(state.ToolBlock{ID: "", Name: "read", Status: state.ToolBlockRunning})

	if !s.UpdateToolBlock("synthesized-id", state.ToolBlockCompleted, "x", nil) {
		t.Fatal("UpdateToolBlock should fall back to the first running block when id mismatches")
	}
	if got := s.Messages[0].ToolBlocks[0].Status; got != state.ToolBlockCompleted {
		t.Fatalf("status not updated: got %v, want completed", got)
	}
}

func TestSession_UpdateToolBlock_NoRunning(t *testing.T) {
	s := state.NewSession()
	s.StartAssistant("", "")
	s.AppendToolBlock(state.ToolBlock{ID: "t1", Name: "read", Status: state.ToolBlockCompleted})

	if s.UpdateToolBlock("nonexistent", state.ToolBlockCompleted, "x", nil) {
		t.Error("UpdateToolBlock should return false when no running blocks exist for fallback")
	}
}

func TestSession_UpdateToolBlock_StoresDiagnostics(t *testing.T) {
	s := state.NewSession()
	s.StartAssistant("", "")
	s.AppendToolBlock(state.ToolBlock{ID: "t1", Name: "edit", Status: state.ToolBlockRunning})
	diags := []state.ToolDiagnostic{{StartLine: 1, StartCol: 1, Severity: "error", Message: "undefined: x"}}
	if !s.UpdateToolBlock("t1", state.ToolBlockCompleted, "ok", diags) {
		t.Fatal("UpdateToolBlock failed")
	}
	got := s.Messages[0].ToolBlocks[0].Diagnostics
	if len(got) != 1 || got[0].Message != "undefined: x" {
		t.Fatalf("diagnostics not stored: %+v", got)
	}
}

func TestSession_FinishAssistant(t *testing.T) {
	s := state.NewSession()
	s.StartAssistant("", "")
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

func TestToggleLastToolBlock(t *testing.T) {
	s := state.NewSession()
	s.StartAssistant("", "")
	s.AppendToolBlock(state.ToolBlock{ID: "t1", Name: "read", Status: state.ToolBlockRunning})
	s.UpdateToolBlock("t1", state.ToolBlockCompleted, "line1\nline2", nil)
	// No auto-expand — tools start collapsed regardless of result size.
	if s.Messages[0].ToolBlocks[0].Expanded {
		t.Fatal("expected tool block to stay collapsed after completion")
	}
	// toggle on
	s.ToggleLastToolBlock()
	if !s.Messages[0].ToolBlocks[0].Expanded {
		t.Fatal("expected toggle on")
	}
	// toggle off
	s.ToggleLastToolBlock()
	if s.Messages[0].ToolBlocks[0].Expanded {
		t.Fatal("expected toggle off")
	}
}

func TestNoAutoExpandLongResult(t *testing.T) {
	s := state.NewSession()
	s.StartAssistant("", "")
	s.AppendToolBlock(state.ToolBlock{ID: "t2", Name: "read", Status: state.ToolBlockRunning})
	result := strings.Repeat("line\n", 11)
	s.UpdateToolBlock("t2", state.ToolBlockCompleted, result, nil)
	if s.Messages[0].ToolBlocks[0].Expanded {
		t.Fatal("expected no auto-expand")
	}
}

func TestSession_Clear(t *testing.T) {
	s := state.NewSession()
	s.AppendMessage(state.Message{Role: state.RoleUser, Text: "hello"})
	s.StartAssistant("", "")
	s.AppendAssistantDelta("hi")
	s.Clear()
	if len(s.Messages) != 0 {
		t.Fatalf("want 0 messages after Clear, got %d", len(s.Messages))
	}
	// Should be safe to start a new assistant after Clear.
	s.StartAssistant("", "")
	s.AppendAssistantDelta("ok")
	s.FinishAssistant()
	if s.Messages[0].Text != "ok" {
		t.Fatalf("want 'ok', got %q", s.Messages[0].Text)
	}
}

func TestAddRemoveDiff(t *testing.T) {
	s := state.NewSession()
	s.AppendMessage(state.Message{Role: state.RoleUser, Text: "hi"})
	s.AddDiffFiles([]state.DiffFile{{Path: "a.txt", Before: "old", After: "new"}})
	if !s.HasDiff() {
		t.Fatal("HasDiff should be true")
	}
	if s.Messages[len(s.Messages)-1].Role != state.RoleDiff {
		t.Fatal("last message should be diff")
	}
	if !s.ToggleLastDiff() || !s.Messages[len(s.Messages)-1].DiffExpanded {
		t.Fatal("ToggleLastDiff should expand")
	}
	s.RemoveDiff()
	if s.HasDiff() {
		t.Fatal("HasDiff should be false after RemoveDiff")
	}
}

func TestAppendAssistantNotice_dedup(t *testing.T) {
	s := state.NewSession()
	s.StartAssistant("build", "m")
	s.AppendAssistantNotice(state.SystemKindRetry, "повтор шага")
	s.AppendAssistantNotice(state.SystemKindRetry, "повтор шага")
	if len(s.Messages[0].Notices) != 1 {
		t.Fatalf("want 1 notice, got %d", len(s.Messages[0].Notices))
	}
}
