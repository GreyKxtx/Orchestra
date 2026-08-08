package tui

import (
	"testing"

	"github.com/orchestra/orchestra/ui/tui/state"
)

func TestRewindToCheckpoint_truncatesMessages(t *testing.T) {
	a := testChromeApp(t)
	a.session.AppendMessage(state.Message{Role: state.RoleUser, Text: "one"})
	a.session.AppendMessage(state.Message{Role: state.RoleAssistant, Text: "a1"})
	a.session.AppendMessage(state.Message{Role: state.RoleUser, Text: "two"})
	a.session.AppendMessage(state.Message{Role: state.RoleAssistant, Text: "a2"})

	a.session.SetMessages(a.session.Messages[:1])
	a.resetStateAfterRewind()

	if len(a.session.Messages) != 1 {
		t.Fatalf("want 1 message, got %d", len(a.session.Messages))
	}
	if a.session.Messages[0].Text != "one" {
		t.Fatalf("text=%q", a.session.Messages[0].Text)
	}
}

func TestRewindCheckpoints_listsUserMessages(t *testing.T) {
	a := testChromeApp(t)
	a.session.AppendMessage(state.Message{Role: state.RoleUser, Text: "alpha"})
	a.session.AppendMessage(state.Message{Role: state.RoleAssistant, Text: "beta"})
	a.session.AppendMessage(state.Message{Role: state.RoleUser, Text: "gamma"})
	cps := a.rewindCheckpoints()
	if len(cps) != 2 {
		t.Fatalf("want 2 checkpoints, got %d", len(cps))
	}
	if cps[1].Label != "gamma" {
		t.Fatalf("second=%q", cps[1].Label)
	}
}
