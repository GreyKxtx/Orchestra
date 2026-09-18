package core

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/orchestra/orchestra/patch/ops"
)

// writeOp is one pending write for the tests below.
func writeOp(path, content string) ops.AnyOp {
	return ops.AnyOp{
		Op:          "file.write_atomic",
		Path:        path,
		WriteAtomic: &ops.WriteAtomicOp{Op: "file.write_atomic", Path: path, Content: content},
	}
}

// seedPending starts a session and gives it pending ops without running a turn.
func seedPending(t *testing.T, c *Core, h *RPCHandler, pending []ops.AnyOp) string {
	t.Helper()
	startP, _ := json.Marshal(SessionStartParams{})
	res, err := h.Handle(context.Background(), "session.start", startP)
	if err != nil {
		t.Fatalf("session.start: %v", err)
	}
	id := res.(*SessionStartResult).SessionID
	sess, err := c.sessions.GetOrLoad(c.workspaceRoot, id)
	if err != nil {
		t.Fatalf("GetOrLoad: %v", err)
	}
	sess.Lock()
	sess.SetPending(pending)
	sess.Unlock()
	return id
}

func discard(t *testing.T, h *RPCHandler, id string, paths []string) *SessionDiscardPendingResult {
	t.Helper()
	p, _ := json.Marshal(SessionDiscardPendingParams{SessionID: id, Paths: paths})
	res, err := h.Handle(context.Background(), "session.discard_pending", p)
	if err != nil {
		t.Fatalf("session.discard_pending: %v", err)
	}
	dr, ok := res.(*SessionDiscardPendingResult)
	if !ok {
		t.Fatalf("expected *SessionDiscardPendingResult, got %T", res)
	}
	return dr
}

func pendingPaths(t *testing.T, c *Core, id string) []string {
	t.Helper()
	sess, err := c.sessions.GetOrLoad(c.workspaceRoot, id)
	if err != nil {
		t.Fatalf("GetOrLoad: %v", err)
	}
	sess.Lock()
	defer sess.Unlock()
	out := []string{}
	for _, op := range sess.CopyPending() {
		out = append(out, op.Path)
	}
	return out
}

// Rejecting one file must leave the rest of the turn pending — otherwise the
// only way to throw away a single bad edit is to throw away the whole turn.
func TestDiscardPending_PathsDropsOnlyThoseFiles(t *testing.T) {
	root := t.TempDir()
	c, h := setupInitializedCore(t, root, &fixedLLM{})

	id := seedPending(t, c, h, []ops.AnyOp{
		writeOp("a.txt", "a"),
		writeOp("b.txt", "b"),
		writeOp("c.txt", "c"),
	})

	dr := discard(t, h, id, []string{"b.txt"})
	if !dr.Discarded {
		t.Fatal("expected Discarded=true when a named file had a pending op")
	}
	if got := len(dr.RemainingOps); got != 2 {
		t.Fatalf("RemainingOps = %d, want 2", got)
	}
	got := pendingPaths(t, c, id)
	if len(got) != 2 || got[0] != "a.txt" || got[1] != "c.txt" {
		t.Fatalf("pending after discard = %v, want [a.txt c.txt]", got)
	}
}

// No paths is the old reject-all: everything goes, nothing remains.
func TestDiscardPending_NoPathsDropsEverything(t *testing.T) {
	root := t.TempDir()
	c, h := setupInitializedCore(t, root, &fixedLLM{})

	id := seedPending(t, c, h, []ops.AnyOp{writeOp("a.txt", "a"), writeOp("b.txt", "b")})

	dr := discard(t, h, id, nil)
	if !dr.Discarded {
		t.Fatal("expected Discarded=true")
	}
	if len(dr.RemainingOps) != 0 {
		t.Fatalf("RemainingOps = %v, want none", dr.RemainingOps)
	}
	if got := pendingPaths(t, c, id); len(got) != 0 {
		t.Fatalf("pending after reject-all = %v, want empty", got)
	}
}

// A path that matches nothing must not report a discard and must not touch
// what is pending: the UI decides from Discarded whether anything happened.
func TestDiscardPending_UnknownPathChangesNothing(t *testing.T) {
	root := t.TempDir()
	c, h := setupInitializedCore(t, root, &fixedLLM{})

	id := seedPending(t, c, h, []ops.AnyOp{writeOp("a.txt", "a")})

	dr := discard(t, h, id, []string{"nope.txt"})
	if dr.Discarded {
		t.Fatal("expected Discarded=false when no pending op matched")
	}
	if got := pendingPaths(t, c, id); len(got) != 1 || got[0] != "a.txt" {
		t.Fatalf("pending = %v, want [a.txt]", got)
	}
}
