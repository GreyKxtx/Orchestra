package core

import (
	"context"
	"testing"

	coresession "github.com/orchestra/orchestra/internal/core/session"
	"github.com/orchestra/orchestra/internal/sessionfile"
	"github.com/orchestra/orchestra/llm"
)

// The turn-boundary invariants. Each of these is a place TurnStarts can go
// stale, and a stale boundary is worse than a missing one: fork and rewind
// would cut a branch in the wrong place and say nothing.

// Invariant 1: the boundary is recorded when a turn STARTS, not when it ends.
// The turn here is cancelled before the agent produces anything, so a turn-end
// computation would record nothing at all — and a mid-turn snapshot firing
// before a crash would leave the session with history it could never map.
func TestSessionMessage_RecordsTheTurnBoundaryBeforeTheTurnRuns(t *testing.T) {
	root := t.TempDir()
	c := setupSessionV2Core(t, root)
	started, err := c.SessionStart(SessionStartParams{})
	if err != nil {
		t.Fatal(err)
	}
	sid := started.SessionID

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.SessionMessage(cancelled, SessionMessageParams{SessionID: sid, Content: "first"}); err == nil {
		t.Fatal("a cancelled turn should not succeed")
	}

	sess, err := c.sessions.GetOrLoad(root, sid)
	if err != nil {
		t.Fatal(err)
	}
	sess.Lock()
	got := sess.TurnStarts()
	histLen := len(sess.CopyHistory())
	sess.Unlock()

	if histLen != 0 {
		t.Fatalf("history = %d, want 0 — this turn produced nothing", histLen)
	}
	if len(got) != 1 || got[0] != 0 {
		t.Fatalf("TurnStarts = %v, want [0] — the boundary must be recorded at turn start even when the turn produces nothing", got)
	}

	// A second turn on top of an existing history records where that history
	// currently ends, so the first turn's output stays inside turn 1.
	sess.Lock()
	sess.ReplaceHistory([]llm.Message{
		{Role: llm.RoleAssistant, Content: "a1"},
		{Role: llm.RoleTool, Content: "tool result"},
		{Role: llm.RoleAssistant, Content: "done"},
	})
	sess.Unlock()

	if _, err := c.SessionMessage(cancelled, SessionMessageParams{SessionID: sid, Content: "second"}); err == nil {
		t.Fatal("a cancelled turn should not succeed")
	}
	sess.Lock()
	got = sess.TurnStarts()
	sess.Unlock()
	if len(got) != 2 || got[0] != 0 || got[1] != 3 {
		t.Fatalf("TurnStarts = %v, want [0 3]", got)
	}
}

// Invariant 2: mid-turn snapshots replace history with PARTIAL-turn content.
// If that path appended a boundary, every 5-second tick inside a long turn
// would invent a turn that never happened.
func TestMidTurnHistorySnapshot_LeavesTurnBoundariesAlone(t *testing.T) {
	root := t.TempDir()
	sess := coresession.NewWithID("mid-turn")
	sess.Lock()
	sess.AppendTurnStart(0)
	sess.ReplaceHistory([]llm.Message{{Role: llm.RoleAssistant, Content: "a1"}})
	sess.AppendTurnStart(1)
	sess.Unlock()

	// Two mid-turn ticks inside the second turn.
	persistMidTurnHistory(root, sess, []llm.Message{
		{Role: llm.RoleAssistant, Content: "a1"},
		{Role: llm.RoleAssistant, Content: "partial"},
	})
	persistMidTurnHistory(root, sess, []llm.Message{
		{Role: llm.RoleAssistant, Content: "a1"},
		{Role: llm.RoleAssistant, Content: "partial"},
		{Role: llm.RoleTool, Content: "tool"},
	})

	sess.Lock()
	got := sess.TurnStarts()
	histLen := len(sess.CopyHistory())
	sess.Unlock()

	if len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Fatalf("TurnStarts = %v, want [0 1] — a mid-turn snapshot must not open a new turn", got)
	}
	if histLen != 3 {
		t.Fatalf("history = %d, want 3 — the mid-turn snapshot must still persist history", histLen)
	}
}

// Invariant 3: compaction rewrites history wholesale, so every recorded index
// points into an array that no longer exists. Clearing is the only honest
// answer — after a compaction fork refuses instead of guessing.
func TestCompactedHistory_ClearsTurnBoundaries(t *testing.T) {
	sess := coresession.NewWithID("compacted")
	sess.Lock()
	sess.AppendTurnStart(0)
	sess.ReplaceHistory([]llm.Message{
		{Role: llm.RoleAssistant, Content: "a1"},
		{Role: llm.RoleAssistant, Content: "a2"},
	})
	sess.AppendTurnStart(2)
	sess.Unlock()

	applyCompactedHistory(sess, []llm.Message{
		{Role: llm.RoleUser, Content: "summary of everything so far"},
	})

	sess.Lock()
	got := sess.TurnStarts()
	histLen := len(sess.CopyHistory())
	sess.Unlock()

	if got != nil {
		t.Fatalf("TurnStarts = %v, want nil — compaction invalidates every boundary", got)
	}
	if histLen != 1 {
		t.Fatalf("history = %d, want 1 (the compacted summary)", histLen)
	}
}

// Invariant 4: rewind shortens history, so boundaries pointing past the new
// end must go with it.
func TestSessionRewind_CutsHistoryAtTheBoundaryAndTruncatesTheRest(t *testing.T) {
	root := t.TempDir()
	c := setupSessionV2Core(t, root)
	sid := seedRealisticSession(t, c)

	// Rewind to u2 (ui index 2): the retained UI ends with the u2 prompt, and
	// history keeps turn 1 whole — the same cut fork makes, differing only in
	// whether the prompt itself is kept.
	res, err := c.SessionRewind(SessionRewindParams{SessionID: sid, UIMessageIndex: 2})
	if err != nil {
		t.Fatalf("SessionRewind: %v", err)
	}
	if res.UIMessages != 3 {
		t.Errorf("UIMessages = %d, want 3 (u1, a1, u2)", res.UIMessages)
	}
	if res.HistoryMessages != 3 {
		t.Fatalf("HistoryMessages = %d, want 3 — turn 1 whole, nothing of turn 2", res.HistoryMessages)
	}

	sess, err := c.sessions.GetOrLoad(root, sid)
	if err != nil {
		t.Fatal(err)
	}
	sess.Lock()
	got := sess.TurnStarts()
	hist := sess.CopyHistory()
	sess.Unlock()

	if len(got) != 2 || got[0] != 0 || got[1] != 3 {
		t.Fatalf("TurnStarts = %v, want [0 3] — boundaries past the new end must be dropped", got)
	}
	if hist[2].Content != "a1" {
		t.Errorf("history must end with turn 1's answer, got %q", hist[2].Content)
	}
}

// Sessions written before boundaries existed must rewind exactly as they did
// before: keep the whole history rather than truncate too far.
func TestSessionRewind_FallsBackWhenThereAreNoBoundaries(t *testing.T) {
	root := t.TempDir()
	c := setupSessionV2Core(t, root)
	sid := seedRealisticSession(t, c)

	sess, err := c.sessions.GetOrLoad(root, sid)
	if err != nil {
		t.Fatal(err)
	}
	sess.Lock()
	sess.SetTurnStarts(nil)
	sess.Unlock()

	res, err := c.SessionRewind(SessionRewindParams{SessionID: sid, UIMessageIndex: 2})
	if err != nil {
		t.Fatalf("SessionRewind: %v", err)
	}
	// The old contract: no mappable Nth user message, so keep everything.
	if res.HistoryMessages != 6 {
		t.Fatalf("HistoryMessages = %d, want 6 — without boundaries rewind keeps the full history, exactly as before", res.HistoryMessages)
	}
}

// TestSessionFork_CutsARealisticHistoryAtTheRecordedBoundary is the end-to-end
// counterpart of the sessionfile test: a session whose history holds only
// assistant and tool messages, plus one synthetic role=user hint injected
// mid-run — the shape the product actually writes. Before turn boundaries,
// fork refused this outright.
func TestSessionFork_CutsARealisticHistoryAtTheRecordedBoundary(t *testing.T) {
	root := t.TempDir()
	c := setupSessionV2Core(t, root)
	sid := seedRealisticSession(t, c)

	res, err := c.SessionFork(SessionForkParams{SessionID: sid, UIMessageIndex: 2})
	if err != nil {
		t.Fatalf("SessionFork on a realistically shaped history: %v", err)
	}
	if res.UIMessages != 2 {
		t.Errorf("UIMessages = %d, want 2 (u1, a1)", res.UIMessages)
	}
	if res.HistoryMessages != 3 {
		t.Fatalf("HistoryMessages = %d, want 3 — turn 1 whole", res.HistoryMessages)
	}

	branch, err := sessionfile.Load(root, res.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if branch.History[2].Content != "a1" {
		t.Errorf("branch must end with turn 1's answer, got %q", branch.History[2].Content)
	}
	if len(branch.TurnStarts) != 1 || branch.TurnStarts[0] != 0 {
		t.Errorf("branch TurnStarts = %v, want [0] so the branch is itself forkable", branch.TurnStarts)
	}

	// A compacted parent has no boundaries, and the refusal must say so
	// rather than produce a branch containing what it was meant to leave.
	sess, err := c.sessions.GetOrLoad(root, sid)
	if err != nil {
		t.Fatal(err)
	}
	sess.Lock()
	sess.SetTurnStarts(nil)
	sess.Unlock()
	if _, err := c.SessionFork(SessionForkParams{SessionID: sid, UIMessageIndex: 2}); err == nil {
		t.Fatal("a session with no recorded boundaries must be refused")
	}
}

// seedRealisticSession builds a session whose history is shaped the way the
// product writes it: assistant and tool messages only, plus one synthetic
// role=user message of the kind the agent injects mid-run (LSP hints, retries,
// image carriers). No ordinary user turn appears in history at all, because
// agent_step.go builds a fresh system+user+history slice per request.
//
//	turn 1 (u1) -> history[0:3]
//	turn 2 (u2) -> history[3:5]  (starts with a synthetic role=user hint)
//	turn 3 (u3) -> history[5:6]
func seedRealisticSession(t *testing.T, c *Core) string {
	t.Helper()
	started, err := c.SessionStart(SessionStartParams{})
	if err != nil {
		t.Fatal(err)
	}
	sid := started.SessionID

	if _, err := c.SessionUISync(SessionUISyncParams{
		SessionID: sid,
		Title:     "real session",
		UIMessages: []sessionfile.UIMessage{
			{Role: "user", Text: "u1"},
			{Role: "assistant", Text: "a1"},
			{Role: "user", Text: "u2"},
			{Role: "assistant", Text: "a2"},
			{Role: "user", Text: "u3"},
			{Role: "assistant", Text: "a3"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	sess, err := c.sessions.GetOrLoad(c.workspaceRoot, sid)
	if err != nil {
		t.Fatal(err)
	}
	sess.Lock()
	sess.ReplaceHistory([]llm.Message{
		{Role: llm.RoleAssistant, Content: "calling read_file"},
		{Role: llm.RoleTool, Content: "file contents"},
		{Role: llm.RoleAssistant, Content: "a1"},
		{Role: llm.RoleUser, Content: "[lsp] 2 diagnostics in main.go"},
		{Role: llm.RoleAssistant, Content: "a2"},
		{Role: llm.RoleAssistant, Content: "a3"},
	})
	sess.SetTurnStarts([]int{0, 3, 5})
	err = sess.Snapshot(c.workspaceRoot)
	sess.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	return sid
}
