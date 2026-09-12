package core

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
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

// The two session predicates share two terms and answer different questions.
// A review read the trajectory one as "a narrower, duplicated copy" of
// sessionLooksRestoredLocked and prescribed calling that helper instead. This
// pins why that would be wrong, so the merge is not attempted again.
func TestSessionPredicates_AnswerDifferentQuestions(t *testing.T) {
	c, _ := setupInitializedCore(t, t.TempDir(), &fixedLLM{})

	// A session holding only a todo list: durable state worth re-reading, but
	// no turn has ever run, so it cannot have a log.
	sess := c.sessions.Create()
	sess.Lock()
	sess.SetTodos([]tools.TodoItem{{ID: "1", Content: "write the thing", Status: "pending"}})
	restored := sessionLooksRestoredLocked(sess)
	nothingYet := sessionHasNothingToRecordLocked(sess)
	sess.Unlock()

	if !restored {
		t.Error("a session with todos carries durable state — sessionLooksRestoredLocked must say so")
	}
	if !nothingYet {
		t.Error("a session that has never run a turn has nothing recorded — it does not predate the log")
	}
	if restored == !nothingYet {
		t.Error("the predicates agreed here; if they ever do for every input, one of them is wrong")
	}

	// And a plan path alone behaves the same way.
	sess2 := c.sessions.Create()
	sess2.Lock()
	sess2.SetPlanPath(".orchestra/plans/p.md")
	restored2 := sessionLooksRestoredLocked(sess2)
	nothingYet2 := sessionHasNothingToRecordLocked(sess2)
	sess2.Unlock()
	if !restored2 || !nothingYet2 {
		t.Errorf("a session with only a plan path: restored=%v nothingYet=%v, want both true", restored2, nothingYet2)
	}
}
