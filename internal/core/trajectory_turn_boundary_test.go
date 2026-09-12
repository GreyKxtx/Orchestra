package core

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/orchestra/orchestra/internal/trajectory"
)

// The log records every notification a turn produced, and nothing that says
// where the turn began or ended. A reader infers both from the first and last
// event it happens to see, which is wrong whenever a turn produced no
// notifications at all, and gives no duration for the turn as a whole.
//
// This is the one gap on the follow-ups list that cannot be closed
// retroactively: a boundary missing from a session recorded today is missing
// from it forever. The finding that raised it asked for the decision to be
// made before the view shipped; it was not, so every session recorded since
// has the hole.

func turnBoundaryEvents(t *testing.T, root, sessionID string) []trajectory.Event {
	t.Helper()
	events, recorded, err := trajectory.Read(root, sessionID)
	if err != nil {
		t.Fatalf("trajectory.Read: %v", err)
	}
	if !recorded {
		t.Fatal("the turn recorded no log at all")
	}
	return events
}

func eventField(t *testing.T, e trajectory.Event, key string) any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(e.Data, &m); err != nil {
		t.Fatalf("event %d data is not an object: %v", e.Seq, err)
	}
	return m[key]
}

func TestTrajectory_ATurnRecordsWhereItStartedAndEnded(t *testing.T) {
	root := t.TempDir()
	c, _ := setupInitializedCore(t, root, &fixedLLM{})

	start, err := c.SessionStart(SessionStartParams{})
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	if _, err := c.SessionMessage(context.Background(), SessionMessageParams{SessionID: start.SessionID, Content: "hello"}); err != nil {
		t.Fatalf("SessionMessage: %v", err)
	}

	events := turnBoundaryEvents(t, root, start.SessionID)
	if len(events) < 2 {
		t.Fatalf("want at least a start and an end, got %d events", len(events))
	}

	first, last := events[0], events[len(events)-1]
	if first.Type != trajectory.TypeTurnStart {
		t.Errorf("first event = %q, want %q", first.Type, trajectory.TypeTurnStart)
	}
	if last.Type != trajectory.TypeTurnEnd {
		t.Errorf("last event = %q, want %q", last.Type, trajectory.TypeTurnEnd)
	}

	startTurn := eventField(t, first, "turn_id")
	endTurn := eventField(t, last, "turn_id")
	if startTurn == "" || startTurn == nil {
		t.Error("the start event must carry the turn id")
	}
	if startTurn != endTurn {
		t.Errorf("the boundary events disagree about the turn: %v vs %v", startTurn, endTurn)
	}

	dur, ok := eventField(t, last, "duration_ms").(float64)
	if !ok {
		t.Fatalf("the end event must carry duration_ms, got %#v", eventField(t, last, "duration_ms"))
	}
	if dur < 0 {
		t.Errorf("duration_ms = %v, want >= 0", dur)
	}
	if last.TimeMS-first.TimeMS < 0 {
		t.Errorf("the end is stamped before the start: %d vs %d", last.TimeMS, first.TimeMS)
	}
}

// The boundary is what makes a silent turn visible. Without it a turn that
// emitted no notifications leaves nothing in the log at all, and a reader
// cannot tell it from a turn that never ran.
func TestTrajectory_ASecondTurnGetsItsOwnBoundary(t *testing.T) {
	root := t.TempDir()
	c, _ := setupInitializedCore(t, root, &fixedLLM{})

	start, err := c.SessionStart(SessionStartParams{})
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	for _, text := range []string{"one", "two"} {
		if _, err := c.SessionMessage(context.Background(), SessionMessageParams{SessionID: start.SessionID, Content: text}); err != nil {
			t.Fatalf("SessionMessage(%q): %v", text, err)
		}
	}

	events := turnBoundaryEvents(t, root, start.SessionID)
	starts, ends := 0, 0
	seen := map[any]bool{}
	for _, e := range events {
		switch e.Type {
		case trajectory.TypeTurnStart:
			starts++
			seen[eventField(t, e, "turn_id")] = true
		case trajectory.TypeTurnEnd:
			ends++
		}
	}
	if starts != 2 || ends != 2 {
		t.Errorf("two turns must record two boundaries each way, got %d starts and %d ends", starts, ends)
	}
	if len(seen) != 2 {
		t.Errorf("the two turns must have different ids, got %d distinct", len(seen))
	}
}
