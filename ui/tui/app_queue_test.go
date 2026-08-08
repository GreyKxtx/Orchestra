package tui

import (
	"testing"

	"github.com/orchestra/orchestra/ui/tui/rpcclient"
)

func TestStickyTaskRows_hidesWhenAllDone(t *testing.T) {
	a := testChromeApp(t)
	a.setTodos([]rpcclient.TodoItem{
		{ID: "1", Content: "a", Status: "done"},
		{ID: "2", Content: "b", Status: "done"},
	})
	if got := a.stickyTaskRows(); got != 0 {
		t.Fatalf("all done → 0 rows, got %d", got)
	}
}

func TestStickyTaskRows_showsOpenTasks(t *testing.T) {
	a := testChromeApp(t)
	a.setTodos([]rpcclient.TodoItem{
		{ID: "1", Content: "a", Status: "done"},
		{ID: "2", Content: "b", Status: "pending"},
	})
	if got := a.stickyTaskRows(); got < 3 {
		t.Fatalf("one open task → lead+task+footer (≥3), got %d", got)
	}
}

func TestEnqueueMessage_whileBusy(t *testing.T) {
	a := testChromeApp(t)
	a.beginAgentTurn()
	a.enqueueMessage("follow up")
	if a.queuedMessageCount() != 1 {
		t.Fatalf("expected 1 queued message, got %d", a.queuedMessageCount())
	}
	a.enqueueMessage("second")
	if a.queuedMessageCount() != 2 {
		t.Fatalf("expected 2 queued messages, got %d", a.queuedMessageCount())
	}
}

func TestFinishAgentTurn_keepsQueueWithoutRPC(t *testing.T) {
	a := testChromeApp(t)
	a.beginAgentTurn()
	a.enqueueMessage("next prompt")
	a.finishAgentTurn()
	if a.queuedMessageCount() != 1 {
		t.Fatalf("without core RPC queue must stay intact, got %d", a.queuedMessageCount())
	}
	if a.turn.ShowBusySpinner() {
		t.Fatal("finishAgentTurn should clear busy state")
	}
}
