package tui

import (
	"errors"
	"strings"
	"testing"
)

func TestParseMCPPromptCommand(t *testing.T) {
	cases := []struct {
		in                   string
		server, name, args   string
		ok                   bool
	}{
		{"/mcp:linear:triage", "linear", "triage", "", true},
		{"/mcp:linear:triage ENG-1", "linear", "triage", "ENG-1", true},
		{"/mcp:linear:triage ENG-1 looks flaky", "linear", "triage", "ENG-1 looks flaky", true},
		{"  /mcp:linear:triage  ENG-1  ", "linear", "triage", "ENG-1", true},

		// Not prompt commands.
		{"/mcp", "", "", "", false},           // the built-in server dialog
		{"/mcp:linear", "", "", "", false},    // no prompt name
		{"/model", "", "", "", false},
		{"just a message", "", "", "", false},
		{"/mcp::triage", "", "", "", false},   // empty server
		{"/mcp:linear:", "", "", "", false},   // empty prompt name
	}
	for _, tc := range cases {
		server, name, args, ok := parseMCPPromptCommand(tc.in)
		if ok != tc.ok || server != tc.server || name != tc.name || args != tc.args {
			t.Errorf("parseMCPPromptCommand(%q) = %q/%q/%q/%v, want %q/%q/%q/%v",
				tc.in, server, name, args, ok, tc.server, tc.name, tc.args, tc.ok)
		}
	}
}

func TestMCPPrompt_TypedWithArgsRunsAndSubmitsTheRenderedText(t *testing.T) {
	a, f := testCoreApp(t)
	a.currentSessionID = "sess-1"
	f.mcpPromptText = "Please triage ENG-1: it looks flaky."

	a.input.SetValue("/mcp:linear:triage ENG-1 looks flaky")
	_, cmd, handled := a.handleEnter()
	if !handled {
		t.Fatal("Enter on a prompt command must be handled")
	}
	// The command fetches the prompt; its message then submits the turn.
	msg := cmd()
	ready, ok := msg.(mcpPromptReadyMsg)
	if !ok {
		t.Fatalf("msg = %#v, want mcpPromptReadyMsg", msg)
	}
	if f.mcpPromptGot != "linear:triage ENG-1 looks flaky" {
		t.Errorf("core received %q", f.mcpPromptGot)
	}

	next, _ := a.handleMCPPromptMsg(ready)
	execCmdTree(next)

	msgs := f.recordedSessionMessages()
	if len(msgs) != 1 {
		t.Fatalf("session messages = %d, want 1", len(msgs))
	}
	if msgs[0].Query != f.mcpPromptText {
		t.Errorf("query = %q, want the rendered prompt", msgs[0].Query)
	}
	// The literal slash command must never reach the model.
	if strings.Contains(msgs[0].Query, "/mcp:") {
		t.Errorf("the raw command leaked into the turn: %q", msgs[0].Query)
	}
}

func TestMCPPrompt_FailureIsShownInChatNotSentToTheModel(t *testing.T) {
	a, f := testCoreApp(t)
	a.currentSessionID = "sess-1"
	f.mcpPromptErr = errors.New(`prompt argument "id" is required`)

	a.input.SetValue("/mcp:linear:triage")
	_, cmd, _ := a.handleEnter()
	msg := cmd()
	if _, ok := msg.(mcpPromptFailedMsg); !ok {
		t.Fatalf("msg = %#v, want mcpPromptFailedMsg", msg)
	}
	next, handled := a.handleMCPPromptMsg(msg)
	if !handled || next != nil {
		t.Fatalf("a failure must be handled without starting a turn (cmd=%v)", next)
	}
	if len(f.recordedSessionMessages()) != 0 {
		t.Error("a failed prompt must not send anything to the model")
	}
	last := a.session.Messages[len(a.session.Messages)-1]
	if !strings.Contains(last.Text, "is required") {
		t.Errorf("chat does not explain the failure: %q", last.Text)
	}
}

func TestMCPPrompt_BuiltInSlashMCPStillOpensTheDialog(t *testing.T) {
	// "/mcp" is one character away from a prompt command; the server dialog
	// must not be swallowed by the prompt route.
	if _, _, _, ok := parseMCPPromptCommand("/mcp"); ok {
		t.Fatal("/mcp must not parse as a prompt command")
	}
}
