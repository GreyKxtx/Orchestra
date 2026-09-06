package tui

import (
	"testing"

	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/view"
)

func TestForkCheckpoints_ListsUserMessages(t *testing.T) {
	a := testChromeApp(t)
	a.session.AppendMessage(state.Message{Role: state.RoleUser, Text: "alpha"})
	a.session.AppendMessage(state.Message{Role: state.RoleAssistant, Text: "beta"})
	a.session.AppendMessage(state.Message{Role: state.RoleUser, Text: "gamma"})

	// Fork reuses rewind's checkpoint list: both branch at user turns.
	cps := a.rewindCheckpoints()
	if len(cps) != 2 {
		t.Fatalf("want 2 checkpoints, got %d", len(cps))
	}
	if cps[1].MsgIndex != 2 {
		t.Fatalf("second checkpoint index = %d, want 2", cps[1].MsgIndex)
	}
}

func TestHandleRewindDialog_ForwardsTheCommand(t *testing.T) {
	// Regression: the dialog handler used to discard the tea.Cmd returned by
	// handleRewindSelect, so picking a checkpoint truncated the local view but
	// never sent session.rewind to core or persisted the result.
	//
	// testCoreApp wires a fake core client (as other App-flow tests in this
	// package do) so the command actually reaches the "call core" branch of
	// rewindToCheckpointCmd instead of the offline/no-workspace short-circuit,
	// which would return nil regardless of this bug.
	a, _ := testCoreApp(t)
	a.coreSessionID = "sess-1"
	a.session.AppendMessage(state.Message{Role: state.RoleUser, Text: "one"})
	a.session.AppendMessage(state.Message{Role: state.RoleAssistant, Text: "a1"})

	_, cmd := a.handleRewindDialog(view.RewindDialogMsg{
		Checkpoint: view.RewindCheckpoint{MsgIndex: 0, Label: "one"},
	})
	if cmd == nil {
		t.Fatal("picking a checkpoint must return the command that persists and notifies core")
	}
}

func TestHandleRewindDialog_ForkBranchIsRoutedToFork(t *testing.T) {
	a, _ := testCoreApp(t)
	a.coreSessionID = "sess-1"
	a.session.AppendMessage(state.Message{Role: state.RoleUser, Text: "one"})
	a.session.AppendMessage(state.Message{Role: state.RoleAssistant, Text: "a1"})
	a.session.AppendMessage(state.Message{Role: state.RoleUser, Text: "two"})

	// A fork must not truncate the current view — that is rewind's job.
	_, cmd := a.handleRewindDialog(view.RewindDialogMsg{
		Fork:       true,
		Checkpoint: view.RewindCheckpoint{MsgIndex: 2, Label: "two"},
	})
	if cmd == nil {
		t.Fatal("fork must return a command")
	}
	if len(a.session.Messages) != 3 {
		t.Fatalf("fork must leave the current session untouched, got %d messages", len(a.session.Messages))
	}
}
