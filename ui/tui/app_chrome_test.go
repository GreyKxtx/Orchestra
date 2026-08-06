package tui

import (
	"testing"
	"time"

	"github.com/orchestra/orchestra/ui/tui/rpcclient"
	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/view"
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

func TestHandleCtrlTCascade_expandsToolsBeforeTodos(t *testing.T) {
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
		t.Fatal("cascade should handle tools")
	}
	if !a.session.Messages[0].ToolsExpanded {
		t.Fatal("expected ToolsExpanded after cascade")
	}
	if !a.chat.IsTurnExpanded(view.MessageKey(a.session.Messages[0])) {
		t.Fatal("expected chat expand state")
	}
	if a.taskPanelOpen {
		t.Fatal("todos must not monopolize Ctrl+T when tools exist")
	}
}

func TestHandleCtrlTCascade_opensTasksWhenNoToolsOrDiff(t *testing.T) {
	a := testChromeApp(t)
	a.todos = []rpcclient.TodoItem{{ID: "1", Content: "task", Status: "pending"}}
	a.setTodos(a.todos)

	if !a.handleCtrlTCascade() {
		t.Fatal("cascade should open tasks")
	}
	if !a.taskPanelOpen {
		t.Fatal("expected task panel open")
	}
	// Second press closes.
	if !a.handleCtrlTCascade() {
		t.Fatal("cascade should close tasks")
	}
	if a.taskPanelOpen {
		t.Fatal("expected task panel closed")
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
