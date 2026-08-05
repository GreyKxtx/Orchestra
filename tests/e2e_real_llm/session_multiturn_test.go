package e2e_real_llm

import (
	"strings"
	"testing"
)

// TestRealLLMSessionMultiTurn exercises session.start → session.message → reopen →
// second session.message over stdio core with a real LLM.
//
// Turn 1: ask the model to read main.go and name the function called from main.
// Reopen: session.start(session_id) simulates TUI session picker.
// Turn 2: ask the model to repeat the function name from the previous turn (requires history).
func TestRealLLMSessionMultiTurn(t *testing.T) {
	requireE2ELLM(t)

	projectDir := setupTestProject(t)
	client := startCoreRPC(t, projectDir)
	client.initialize(projectDir)

	sessionID := client.sessionStart("")
	t.Logf("session_id=%s", sessionID)

	turn1Query := "Read main.go. What is the name of the function called from main()? Reply with ONLY the function name, nothing else."
	client.sessionMessage(sessionID, turn1Query)
	turn1Events := client.drainAgentEvents()
	turn1ID := firstAgentEventTurnID(turn1Events)
	if turn1ID == "" {
		t.Fatal("turn 1: expected agent/event with turn_id")
	}
	if !agentEventsWithTurnID(turn1Events, sessionID, turn1ID) {
		t.Fatalf("turn 1: agent/event missing session_id=%q turn_id=%q", sessionID, turn1ID)
	}

	histLen := client.sessionHistoryLen(sessionID)
	if histLen == 0 {
		t.Fatal("expected non-empty session history after turn 1")
	}
	t.Logf("history messages after turn 1: %d", histLen)

	// Simulate TUI reopen: new session.start with known id restores agent history.
	reopenedID := client.sessionStart(sessionID)
	if reopenedID != sessionID {
		t.Fatalf("reopen session_id mismatch: got %q want %q", reopenedID, sessionID)
	}
	if client.sessionHistoryLen(sessionID) == 0 {
		t.Fatal("history lost after session.start reopen")
	}

	turn2Query := "In your previous answer in this session, what function name did you give? Reply with ONLY that name, nothing else."
	client.sessionMessage(sessionID, turn2Query)
	turn2Events := client.drainAgentEvents()
	turn2ID := firstAgentEventTurnID(turn2Events)
	if turn2ID == "" {
		t.Fatal("turn 2: expected agent/event with turn_id")
	}
	if turn2ID == turn1ID {
		t.Fatalf("turn 2 should have a new turn_id, got same as turn 1: %s", turn1ID)
	}
	if !agentEventsWithTurnID(turn2Events, sessionID, turn2ID) {
		t.Fatalf("turn 2: agent/event missing session_id=%q turn_id=%q", sessionID, turn2ID)
	}

	if client.sessionHistoryLen(sessionID) <= histLen {
		t.Fatalf("history should grow after turn 2: before=%d after=%d", histLen, client.sessionHistoryLen(sessionID))
	}

	// Best-effort: second turn should recall "greet" from project fixture.
	foundGreet := false
	for _, ev := range turn2Events {
		if ev.Method != "agent/event" {
			continue
		}
		if strings.Contains(strings.ToLower(string(ev.Params)), "greet") {
			foundGreet = true
			break
		}
	}
	if !foundGreet {
		t.Logf("turn 2 did not stream 'greet' in events (model may answer differently); history growth + distinct turn_id still verified")
	} else {
		t.Logf("turn 2 recalled 'greet' from session history")
	}
}
