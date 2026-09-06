package core

import (
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/sessionfile"
	"github.com/orchestra/orchestra/llm"
)

// seedForkableSession starts a session and gives it two answered user turns.
func seedForkableSession(t *testing.T, c *Core) string {
	t.Helper()
	started, err := c.SessionStart(SessionStartParams{})
	if err != nil {
		t.Fatal(err)
	}
	sid := started.SessionID

	if _, err := c.SessionUISync(SessionUISyncParams{
		SessionID: sid,
		Title:     "original task",
		UIMessages: []sessionfile.UIMessage{
			{Role: "user", Text: "u1"},
			{Role: "assistant", Text: "a1"},
			{Role: "user", Text: "u2"},
			{Role: "assistant", Text: "a2"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	sess, err := c.sessions.GetOrLoad(c.workspaceRoot, sid)
	if err != nil {
		t.Fatal(err)
	}
	// History as the product writes it: assistant and tool messages only. The
	// user's prompts are NOT here — agent_step.go builds a fresh
	// system+user+history slice per request — so the two turns are located by
	// their recorded boundaries, not by counting role=user entries.
	sess.Lock()
	sess.ReplaceHistory([]llm.Message{
		{Role: llm.RoleAssistant, Content: "calling read_file"},
		{Role: llm.RoleTool, Content: "a1"},
		{Role: llm.RoleAssistant, Content: "calling grep"},
		{Role: llm.RoleTool, Content: "a2"},
	})
	sess.SetTurnStarts([]int{0, 2})
	err = sess.Snapshot(c.workspaceRoot)
	sess.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	return sid
}

func TestSessionFork_CreatesABranchAndLeavesTheParentIntact(t *testing.T) {
	root := t.TempDir()
	c := setupSessionV2Core(t, root)
	sid := seedForkableSession(t, c)

	res, err := c.SessionFork(SessionForkParams{SessionID: sid, UIMessageIndex: 2})
	if err != nil {
		t.Fatalf("SessionFork: %v", err)
	}

	if res.SessionID == sid || res.SessionID == "" {
		t.Fatalf("fork must return a new session id, got %q", res.SessionID)
	}
	if res.ParentID != sid {
		t.Errorf("ParentID = %q, want %q", res.ParentID, sid)
	}
	// Exclusive boundary: u1 + a1 survive, u2 does not.
	if res.UIMessages != 2 {
		t.Errorf("UIMessages = %d, want 2", res.UIMessages)
	}
	if res.HistoryMessages != 2 {
		t.Errorf("HistoryMessages = %d, want 2", res.HistoryMessages)
	}

	branch, err := sessionfile.Load(root, res.SessionID)
	if err != nil {
		t.Fatalf("branch must be on disk: %v", err)
	}
	if branch.ParentID != sid || branch.ForkedFromIndex != 2 {
		t.Errorf("lineage = %q/%d", branch.ParentID, branch.ForkedFromIndex)
	}

	// The whole point of fork over rewind: the parent still has everything.
	parent, err := sessionfile.Load(root, sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(parent.UIMessages) != 4 {
		t.Fatalf("parent UIMessages = %d, want 4 — fork must not truncate the parent", len(parent.UIMessages))
	}
	if len(parent.History) != 4 {
		t.Fatalf("parent History = %d, want 4", len(parent.History))
	}
}

func TestSessionFork_BranchIsLoadableAsASession(t *testing.T) {
	root := t.TempDir()
	c := setupSessionV2Core(t, root)
	sid := seedForkableSession(t, c)

	res, err := c.SessionFork(SessionForkParams{SessionID: sid, UIMessageIndex: 2})
	if err != nil {
		t.Fatal(err)
	}

	// The branch is deliberately not registered in the Manager; the client's
	// next session.start must pick it up from disk. Assert the "one owner per
	// id" property directly rather than leaving it to inspection: if fork had
	// registered the branch, session.start would hand back that instance and
	// the on-disk snapshot would have a second owner.
	if _, err := c.sessions.Get(res.SessionID); err == nil {
		t.Fatal("fork must not register the branch with the Manager — session.start would then be the second owner of one id")
	}

	started, err := c.SessionStart(SessionStartParams{SessionID: res.SessionID})
	if err != nil {
		t.Fatalf("session.start on the branch: %v", err)
	}
	if !started.Restored {
		t.Error("the branch should be restored from disk, not created fresh")
	}
	got, err := c.SessionGet(SessionGetParams{SessionID: res.SessionID})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.UIMessages) != 2 {
		t.Fatalf("branch UIMessages = %d, want 2", len(got.UIMessages))
	}
}

func TestSessionFork_RejectsBadInput(t *testing.T) {
	root := t.TempDir()
	c := setupSessionV2Core(t, root)
	sid := seedForkableSession(t, c)

	if _, err := c.SessionFork(SessionForkParams{SessionID: "", UIMessageIndex: 2}); err == nil {
		t.Error("empty session id must be refused")
	}
	if _, err := c.SessionFork(SessionForkParams{SessionID: sid, UIMessageIndex: 1}); err == nil {
		t.Error("an assistant message is not a fork point")
	}
	if _, err := c.SessionFork(SessionForkParams{SessionID: sid, UIMessageIndex: 0}); err == nil {
		t.Error("index 0 must be refused")
	}
	if _, err := c.SessionFork(SessionForkParams{SessionID: sid, UIMessageIndex: 99}); err == nil {
		t.Error("out-of-range index must be refused")
	}
}

func TestSessionSearch_FindsAcrossSessions(t *testing.T) {
	root := t.TempDir()
	c := setupSessionV2Core(t, root)
	sid := seedForkableSession(t, c)

	res, err := c.SessionSearch(SessionSearchParams{Query: "u2"})
	if err != nil {
		t.Fatalf("SessionSearch: %v", err)
	}
	if len(res.Hits) != 1 {
		t.Fatalf("hits = %d, want 1: %+v", len(res.Hits), res.Hits)
	}
	if res.Hits[0].SessionID != sid || res.Hits[0].Index != 2 {
		t.Errorf("hit = %+v", res.Hits[0])
	}
	if !strings.Contains(res.Hits[0].Snippet, "u2") {
		t.Errorf("snippet = %q", res.Hits[0].Snippet)
	}

	if _, err := c.SessionSearch(SessionSearchParams{Query: " "}); err == nil {
		t.Error("an empty query must be refused")
	}
}
