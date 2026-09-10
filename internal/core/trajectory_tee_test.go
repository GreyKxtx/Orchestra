package core

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/orchestra/orchestra/internal/trajectory"
)

func TestTeeToTrajectory_RecordsWhatItForwards(t *testing.T) {
	root := t.TempDir()
	w, err := trajectory.NewWriter(root, "s1")
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	var forwarded []string
	notify := func(method string, params any) { forwarded = append(forwarded, method) }

	tee := teeToTrajectory(notify, w)
	tee("agent/event", map[string]any{"type": "tool_call_start", "step": 1})
	tee("exec/output_chunk", map[string]any{"chunk": "hello"})

	// Forwarding must be unchanged — the tee is additive.
	if len(forwarded) != 2 || forwarded[0] != "agent/event" || forwarded[1] != "exec/output_chunk" {
		t.Fatalf("forwarded = %v, want [agent/event exec/output_chunk]", forwarded)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	events, recorded, err := trajectory.Read(root, "s1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !recorded || len(events) != 2 {
		t.Fatalf("recorded=%v len=%d, want true and 2", recorded, len(events))
	}
	if events[0].Type != "agent/event" || events[1].Type != "exec/output_chunk" {
		t.Errorf("types = %q,%q", events[0].Type, events[1].Type)
	}
	var first map[string]any
	if err := json.Unmarshal(events[0].Data, &first); err != nil {
		t.Fatalf("payload not stored as an object: %v", err)
	}
	if first["type"] != "tool_call_start" {
		t.Errorf("payload type = %v, want tool_call_start", first["type"])
	}
}

func TestTeeToTrajectory_NilWriterForwardsAndDoesNotPanic(t *testing.T) {
	// A session with no writer (a one-shot agent.run has no session id) must
	// keep notifying. Recording is best-effort; delivery is not.
	var forwarded int
	tee := teeToTrajectory(func(string, any) { forwarded++ }, nil)
	tee("agent/event", map[string]any{"type": "done"})
	if forwarded != 1 {
		t.Errorf("forwarded = %d, want 1", forwarded)
	}
}

// TestSessionTurn_LeavesATrajectoryOnDisk drives one session.message turn
// against a scripted LLM (no notifier attached, exactly like
// setupInitializedCore's other callers) and asserts the sidecar log left on
// disk reflects the turn.
//
// The harness attaches no notifier, so p.OnEvent is nil going into
// prepareAgentLaunch — this is precisely the configuration Step 5's tee must
// still record in. If this test finds no log, the bug is a nil guard, not a
// fault in the harness.
func TestSessionTurn_LeavesATrajectoryOnDisk(t *testing.T) {
	root := t.TempDir()

	// A final response with no patches: the agent settles on step 1 without
	// calling any tool, emitting a step_done("final") notification along the
	// way — the terminal marker for this turn.
	finalStep := `{"type":"final","final":{"patches":[]}}`
	_, h := setupInitializedCore(t, root, &fixedLLM{steps: []string{finalStep}})

	startP, _ := json.Marshal(SessionStartParams{})
	res, err := h.Handle(context.Background(), "session.start", startP)
	if err != nil {
		t.Fatalf("session.start: %v", err)
	}
	sessionID := res.(*SessionStartResult).SessionID

	msgP, _ := json.Marshal(SessionMessageParams{
		SessionID: sessionID,
		Content:   "say hello",
	})
	if _, err := h.Handle(context.Background(), "session.message", msgP); err != nil {
		t.Fatalf("session.message: %v", err)
	}

	events, recorded, err := trajectory.Read(root, sessionID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !recorded {
		t.Fatal("expected recorded=true; the session should have left a sidecar log")
	}
	if len(events) == 0 {
		t.Fatal("expected at least one recorded event")
	}

	// Seq must be contiguous from 1 — a gap would mean an event failed to
	// write, which trajectory.Writer treats as a visible defect, not silent
	// data loss.
	for i, ev := range events {
		if want := int64(i + 1); ev.Seq != want {
			t.Fatalf("events[%d].Seq = %d, want %d (contiguous from 1)", i, ev.Seq, want)
		}
	}

	// The turn's terminal marker: an agent/event whose payload says the step
	// finished as "final". (Note: with this fixedLLM script — a single-step
	// final response with no tool calls and no provider Usage — the agent
	// never emits a StreamEventDone/"done"-typed event; that only happens on
	// the real streaming path. step_done/"final" is the equivalent marker
	// here: it is what signals this turn's terminal step.)
	foundStepDone := false
	for _, ev := range events {
		if ev.Type != "agent/event" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(ev.Data, &payload); err != nil {
			continue
		}
		if payload["type"] == "step_done" && payload["content"] == "final" {
			foundStepDone = true
			break
		}
	}
	if !foundStepDone {
		t.Errorf("expected an agent/event with payload type=step_done content=final, got events: %+v", events)
	}
}
