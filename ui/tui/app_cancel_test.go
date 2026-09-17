package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/orchestra/orchestra/ui/tui/rpcclient"
)

// Esc cancels the turn, and the cancelled call comes back as "context
// canceled" — the Go error for the context the TUI itself closed. The chat
// showed it as a failure: "✗ Error · context canceled" right under "● Info ·
// ход отменён". A turn the user stopped did not go wrong.
func TestEscCancel_DoesNotReportTheCancellationAsAnError(t *testing.T) {
	a, _ := startedTurnApp(t)
	a.activeCancel = func() {}

	a.routeKey(tea.KeyMsg{Type: tea.KeyEsc})
	a.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventError, Err: "context canceled"})
	a.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventAgentRunCompleted})
	a.chat.SetMessages(a.session.Messages)

	plain := stripANSIForTest(a.chat.View())
	if !strings.Contains(plain, "отменён") {
		t.Fatalf("the cancel notice is gone: %s", plain)
	}
	if strings.Contains(plain, "context canceled") {
		t.Errorf("a turn the user cancelled is reported as an error: %s", plain)
	}

	// An error that is not the cancellation still reaches the user.
	b, _ := startedTurnApp(t)
	b.activeCancel = func() {}
	b.routeKey(tea.KeyMsg{Type: tea.KeyEsc})
	b.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventError, Err: "LLM Endpoint unreachable at http://127.0.0.1:1"})
	b.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventAgentRunCompleted})
	b.chat.SetMessages(b.session.Messages)
	if !strings.Contains(stripANSIForTest(b.chat.View()), "unreachable") {
		t.Error("a real error during a cancelled turn must still be shown")
	}

	// The next turn is not silenced by the cancel that came before it.
	c, _ := startedTurnApp(t)
	c.activeCancel = func() {}
	c.routeKey(tea.KeyMsg{Type: tea.KeyEsc})
	c.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventAgentRunCompleted})
	c.submitUserMessage("ещё раз")
	c.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventError, Err: "context canceled"})
	c.handleRPCEvent(rpcclient.Event{Kind: rpcclient.EventAgentRunCompleted})
	c.chat.SetMessages(c.session.Messages)
	if !strings.Contains(stripANSIForTest(c.chat.View()), "context canceled") {
		t.Error("the cancel flag leaked into the next turn and hid its error")
	}
}
