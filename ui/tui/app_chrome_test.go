package tui

import (
	"testing"
	"time"

	"github.com/orchestra/orchestra/ui/tui/rpcclient"
	"github.com/orchestra/orchestra/ui/tui/state"
)

func testChromeApp(t *testing.T) *App {
	t.Helper()
	a, err := NewApp(Config{Model: "test-model", Mode: "build", CWD: "test"})
	if err != nil {
		t.Fatal(err)
	}
	a.width = 80
	a.height = 24
	a.showWelcome = false
	a.layout()
	return a
}

func TestInputEmpty_gatesBareHotkeys(t *testing.T) {
	a := testChromeApp(t)
	if !a.inputEmpty() {
		t.Fatal("fresh input should be empty")
	}
	a.input.SetValue("hello")
	if a.inputEmpty() {
		t.Fatal("non-empty input should not be empty")
	}
	a.input.SetValue("  \t  ")
	if !a.inputEmpty() {
		t.Fatal("whitespace-only input counts as empty for hotkey gates")
	}
}

func TestHandleCtrlTCascade_expandsToolsEvenWithTodos(t *testing.T) {
	a := testChromeApp(t)
	a.todos = []rpcclient.TodoItem{{ID: "1", Content: "task", Status: "pending"}}
	a.setTodos(a.todos)

	started := time.Now()
	a.session.AppendMessage(state.Message{
		Role:      state.RoleAssistant,
		StartedAt: started,
		ToolBlocks: []state.ToolBlock{
			{ID: "t1", Name: "read", Status: state.ToolBlockCompleted, Result: "ok"},
		},
	})
	a.chat.SetMessages(a.session.Messages)

	if !a.handleCtrlTCascade() {
		t.Fatal("cascade should expand tools (tasks live in chat)")
	}
	if !a.session.Messages[0].ToolsExpanded {
		t.Fatal("expected ToolsExpanded after cascade")
	}
}

func TestHandleCtrlTCascade_expandsToolsWhenNoTodos(t *testing.T) {
	a := testChromeApp(t)
	started := time.Now()
	a.session.AppendMessage(state.Message{
		Role:      state.RoleAssistant,
		StartedAt: started,
		ToolBlocks: []state.ToolBlock{
			{ID: "t1", Name: "read", Status: state.ToolBlockCompleted, Result: "ok"},
		},
	})
	a.chat.SetMessages(a.session.Messages)

	if !a.handleCtrlTCascade() {
		t.Fatal("cascade should expand tools")
	}
	if !a.session.Messages[0].ToolsExpanded {
		t.Fatal("expected ToolsExpanded after cascade")
	}
}

func TestSetTodos_upsertsInChatChecklist(t *testing.T) {
	a := testChromeApp(t)
	idx := a.session.StartAssistant("build", "m")
	a.setTodos([]rpcclient.TodoItem{
		{ID: "1", Content: "do thing", Status: "in_progress"},
		{ID: "2", Content: "next", Status: "pending"},
	})
	m := a.session.Messages[idx]
	found := false
	for _, seg := range m.Segments {
		if seg.Kind == state.SegmentTodos && len(seg.Todos) == 2 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected SegmentTodos on assistant turn, segs=%v", m.Segments)
	}
}

func TestCycleShellPerms(t *testing.T) {
	a := testChromeApp(t)
	a.allowExec = false
	a.cycleShellPerms()
	if !a.allowExec {
		t.Fatal("expected allow after cycle")
	}
	a.cycleShellPerms()
	if a.allowExec {
		t.Fatal("expected ask after second cycle")
	}
}

func TestToggleLastReasoning(t *testing.T) {
	a := testChromeApp(t)
	a.session.AppendMessage(state.Message{
		Role:      state.RoleAssistant,
		StartedAt: time.Now(),
		Reasoning: "line1\nline2\nline3\nline4\nline5\nline6\nline7",
		Text:      "answer",
	})
	a.chat.SetMessages(a.session.Messages)

	if !a.toggleLastReasoning() {
		t.Fatal("should toggle reasoning")
	}
	if !a.session.Messages[0].ReasoningExpanded {
		t.Fatal("expected ReasoningExpanded")
	}
	if !a.toggleLastReasoning() {
		t.Fatal("should toggle back")
	}
	if a.session.Messages[0].ReasoningExpanded {
		t.Fatal("expected collapsed again")
	}
}

func TestSyncDiffStateFromSession_restoresLastCommitDiff(t *testing.T) {
	a := testChromeApp(t)
	a.session.AddDiffFiles([]state.DiffFile{{Path: "a.go", Before: "old", After: "new"}})
	a.syncDiffStateFromSession()
	if len(a.lastCommitDiff) != 1 || a.lastCommitDiff[0].Path != "a.go" {
		t.Fatalf("lastCommitDiff=%v", a.lastCommitDiff)
	}
	if !a.diffShown {
		t.Fatal("diffShown should be true after restore")
	}
}
