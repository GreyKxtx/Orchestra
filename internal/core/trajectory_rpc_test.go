package core

import (
	"testing"

	"github.com/orchestra/orchestra/internal/trajectory"
	"github.com/orchestra/orchestra/llm"
)

func TestSessionTrajectory_ReturnsRecordedEvents(t *testing.T) {
	root := t.TempDir()
	w, err := trajectory.NewWriter(root, "s1")
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.Append("agent/event", map[string]any{"type": "done"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	c, _ := setupInitializedCore(t, root, &fixedLLM{})
	res, err := c.SessionTrajectory(SessionTrajectoryParams{SessionID: "s1"})
	if err != nil {
		t.Fatalf("SessionTrajectory: %v", err)
	}
	if !res.Recorded {
		t.Error("Recorded = false, want true")
	}
	if len(res.Events) != 1 {
		t.Fatalf("len(Events) = %d, want 1", len(res.Events))
	}
	if res.Events[0].Seq != 1 || res.Events[0].Type != "agent/event" {
		t.Errorf("Events[0] = %+v", res.Events[0])
	}
}

func TestSessionTrajectory_SessionWithNoLogSaysSoRatherThanReturningEmpty(t *testing.T) {
	root := t.TempDir()
	c, _ := setupInitializedCore(t, root, &fixedLLM{})
	res, err := c.SessionTrajectory(SessionTrajectoryParams{SessionID: "predates-the-log"})
	if err != nil {
		t.Fatalf("SessionTrajectory: %v", err)
	}
	if res.Recorded {
		t.Error("Recorded = true, want false — this session has no log, which is not the same as an empty one")
	}
	if len(res.Events) != 0 {
		t.Errorf("len(Events) = %d, want 0", len(res.Events))
	}
}

func TestSessionTrajectory_EmptySessionIDIsAnError(t *testing.T) {
	c, _ := setupInitializedCore(t, t.TempDir(), &fixedLLM{})
	if _, err := c.SessionTrajectory(SessionTrajectoryParams{}); err == nil {
		t.Error("expected an error for an empty session_id")
	}
}

func TestSessionTrajectory_FreshSessionWithNoTurnIsRecordedAndEmpty(t *testing.T) {
	root := t.TempDir()
	c, _ := setupInitializedCore(t, root, &fixedLLM{})
	started, err := c.SessionStart(SessionStartParams{})
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	res, err := c.SessionTrajectory(SessionTrajectoryParams{SessionID: started.SessionID})
	if err != nil {
		t.Fatalf("SessionTrajectory: %v", err)
	}
	if !res.Recorded {
		t.Error("Recorded = false, want true — a session with no turn yet has had nothing to record; it does not predate the log")
	}
	if len(res.Events) != 0 {
		t.Errorf("len(Events) = %d, want 0", len(res.Events))
	}
}

func TestSessionTrajectory_SessionWithHistoryAndNoLogPredatesTheLog(t *testing.T) {
	root := t.TempDir()
	c, _ := setupInitializedCore(t, root, &fixedLLM{})
	sess := c.sessions.CreateWithID("old-chat")
	sess.Lock()
	sess.History = append(sess.History, llm.Message{Role: llm.RoleUser, Content: "hello from before the log"})
	sess.Unlock()
	res, err := c.SessionTrajectory(SessionTrajectoryParams{SessionID: "old-chat"})
	if err != nil {
		t.Fatalf("SessionTrajectory: %v", err)
	}
	if res.Recorded {
		t.Error("Recorded = true, want false — this session has history but no log, so it predates the log")
	}
	if len(res.Events) != 0 {
		t.Errorf("len(Events) = %d, want 0", len(res.Events))
	}
}

func TestSessionTrajectory_BusyFirstTurnIsRecordedAndEmpty(t *testing.T) {
	root := t.TempDir()
	c, _ := setupInitializedCore(t, root, &fixedLLM{})
	started, err := c.SessionStart(SessionStartParams{})
	if err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	sess, err := c.sessions.Get(started.SessionID)
	if err != nil {
		t.Fatalf("sessions.Get: %v", err)
	}
	// Reproduce the exact window SessionMessage leaves open: the UI message
	// is appended and the session is marked busy before prepareAgentLaunch
	// creates the trajectory sidecar. No sidecar exists at this point.
	sess.Lock()
	sess.AppendUIMessage(buildUserUIMessage("hello", nil))
	sess.SetCancel(func() {})
	sess.Unlock()
	defer func() {
		sess.Lock()
		sess.ClearCancel()
		sess.Unlock()
	}()

	res, err := c.SessionTrajectory(SessionTrajectoryParams{SessionID: started.SessionID})
	if err != nil {
		t.Fatalf("SessionTrajectory: %v", err)
	}
	if !res.Recorded {
		t.Error("Recorded = false, want true — a busy first turn has not predated the log, it is about to create it")
	}
	if len(res.Events) != 0 {
		t.Errorf("len(Events) = %d, want 0", len(res.Events))
	}
}
