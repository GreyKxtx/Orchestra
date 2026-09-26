package tui

import (
	"strings"
	"testing"
)

// A reopened session whose last turn the core did not finish says so and
// offers /resume; the command continues that turn — session.message with
// resume and no new text — and the offer is spent (phase 4.6).
func TestResume_ContinuesTheInterruptedTurn(t *testing.T) {
	a, f := testCoreApp(t)
	a.currentSessionID = "sess-1"
	f.resumableTurnID = "20260926T100000-abcd"

	msg := a.startCoreSession()()
	started, ok := msg.(coreSessionStartedMsg)
	if !ok || started.resumable != f.resumableTurnID {
		t.Fatalf("session.start carries the resumable turn: %#v", msg)
	}
	a.handleCoreSessionStarted(started)
	last := a.session.Messages[len(a.session.Messages)-1]
	if !strings.Contains(last.Text, "/resume") {
		t.Fatalf("the chat says the turn can be resumed: %q", last.Text)
	}

	cmd := a.executePaletteCmd("/resume")
	if cmd == nil {
		t.Fatal("/resume must start a turn")
	}
	if !a.turn.IsRunning() {
		t.Fatal("the turn FSM runs while the resumed turn does")
	}
	execCmdTree(cmd)
	msgs := f.recordedSessionMessages()
	if len(msgs) != 1 || msgs[0].Opts.Resume != f.resumableTurnID || msgs[0].Query != "" || msgs[0].SessionID != "sess-1" {
		t.Fatalf("session.message resumes the turn with no new text: %+v", msgs)
	}
	if a.resumableTurnID != "" {
		t.Fatal("the offer is spent once taken")
	}

	// Nothing to resume: the command says so and starts nothing.
	before := len(f.recordedSessionMessages())
	if cmd := a.executePaletteCmd("/resume"); cmd != nil {
		t.Fatal("no turn to resume, no command")
	}
	if len(f.recordedSessionMessages()) != before {
		t.Fatal("no turn to resume, nothing sent")
	}
	if last := a.session.Messages[len(a.session.Messages)-1]; !strings.Contains(last.Text, "нечего продолжать") {
		t.Fatalf("the chat says there is nothing to resume: %q", last.Text)
	}
}

// A session with nothing to resume gets no offer.
func TestResume_NoOfferWithoutAnInterruptedTurn(t *testing.T) {
	a, _ := testCoreApp(t)
	a.currentSessionID = "sess-2"
	started := a.startCoreSession()().(coreSessionStartedMsg)
	n := len(a.session.Messages)
	a.handleCoreSessionStarted(started)
	for _, m := range a.session.Messages[n:] {
		if strings.Contains(m.Text, "/resume") {
			t.Fatalf("no interrupted turn, yet /resume was offered: %q", m.Text)
		}
	}
}
