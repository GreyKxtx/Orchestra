package core

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/sessionfile"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/patch/cache"
	"github.com/orchestra/orchestra/protocol"
)

// Invariant 5: the agent rewrites history MID-TURN too — compaction inside the
// step loop, and the truncation fallback when compaction errors or refuses to
// converge. That history comes back through Run and is persisted by
// persistSessionTurn, which never reaches SessionCompact. If the boundaries
// recorded at turn start survive that, they index an array that no longer
// exists: fork cuts a silently wrong branch, and rewind — destructive and
// persisted — throws away history a correct cut would have kept.
//
// These tests drive the whole turn (SessionMessage → agent.Run →
// persistSessionTurn) rather than hand-setting Result.HistoryRewritten, so
// they still fail if someone later forgets to thread the flag out of the
// agent loop.

// compactingLLM answers the compaction request with a short summary (so the
// convergence guard adopts it) and every other request with a final step.
type compactingLLM struct {
	compactionCalls int
}

func (l *compactingLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (l *compactingLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	for _, m := range req.Messages {
		if m.Role == llm.RoleSystem && strings.Contains(m.Content, "Context Manager") {
			l.compactionCalls++
			return &llm.CompleteResponse{Message: llm.Message{
				Role:    llm.RoleAssistant,
				Content: "short summary",
			}}, nil
		}
	}
	return &llm.CompleteResponse{Message: llm.Message{
		Role:    llm.RoleAssistant,
		Content: `{"type":"final","final":{"patches":[]}}`,
	}}, nil
}

// setupRewriteCore builds an initialized core whose compaction trigger is
// under test control: compactPct < 0 disables compaction entirely.
func setupRewriteCore(t *testing.T, root string, client llm.Client, compactPct int) *Core {
	t.Helper()
	cfg := config.DefaultConfig(root)
	lspOff := false
	cfg.LSP.Enabled = &lspOff
	cfg.LSP.AutoInstall = "false"
	// A 4 KB history budget with a 30% trigger fires compaction on a history
	// of a few thousand bytes, without needing a real long session.
	cfg.Limits.ContextKB = 4
	cfg.Agent.CompactThresholdPct = compactPct
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatal(err)
	}
	c, err := New(root, Options{LLMClient: client})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	projectID, err := cache.ComputeProjectID(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Initialize(InitializeParams{
		ProjectRoot:     root,
		ProjectID:       projectID,
		ProtocolVersion: protocol.ProtocolVersion,
		OpsVersion:      protocol.OpsVersion,
		ToolsVersion:    protocol.ToolsVersion,
	}); err != nil {
		t.Fatal(err)
	}
	return c
}

// seedPriorTurn gives the session a first turn's worth of history plus the
// boundary that turn recorded, which is what the second turn's rewrite would
// otherwise invalidate in place.
func seedPriorTurn(t *testing.T, c *Core, root, sid string, atoms int, atomSize int) int {
	t.Helper()
	sess, err := c.sessions.GetOrLoad(root, sid)
	if err != nil {
		t.Fatal(err)
	}
	hist := make([]llm.Message, 0, atoms)
	for i := 0; i < atoms; i++ {
		hist = append(hist, llm.Message{
			Role:    llm.RoleAssistant,
			Content: strings.Repeat("x", atomSize),
		})
	}
	sess.Lock()
	sess.AppendTurnStart(0)
	sess.ReplaceHistory(hist)
	sess.Unlock()
	if err := sess.Snapshot(root); err != nil {
		t.Fatal(err)
	}
	return len(hist)
}

func turnStartsOf(t *testing.T, c *Core, root, sid string) []int {
	t.Helper()
	sess, err := c.sessions.GetOrLoad(root, sid)
	if err != nil {
		t.Fatal(err)
	}
	sess.Lock()
	defer sess.Unlock()
	return sess.TurnStarts()
}

// A turn whose run rewrote history must leave the session with NO boundaries,
// so fork and rewind fall back to refusing honestly instead of cutting at an
// index that means nothing.
func TestSessionMessage_ClearsTurnBoundaries_WhenTheTurnRewroteHistory(t *testing.T) {
	root := t.TempDir()
	client := &compactingLLM{}
	c := setupRewriteCore(t, root, client, 30)

	started, err := c.SessionStart(SessionStartParams{})
	if err != nil {
		t.Fatal(err)
	}
	sid := started.SessionID
	seedPriorTurn(t, c, root, sid, 12, 500)

	if _, err := c.SessionMessage(context.Background(), SessionMessageParams{
		SessionID: sid,
		Content:   "second turn",
	}); err != nil {
		t.Fatalf("session.message: %v", err)
	}

	if client.compactionCalls == 0 {
		t.Fatal("compaction never fired during the turn; this test is not exercising the path it claims to")
	}
	if got := turnStartsOf(t, c, root, sid); len(got) != 0 {
		t.Fatalf("TurnStarts = %v, want none — the turn rewrote history, so every recorded index is stale; "+
			"fork would cut a wrong branch and rewind would discard history a correct cut keeps", got)
	}
}

// compactThenFailLLM rewrites history (it answers the compaction request with a
// short summary) and then fails the very next request, which is how the agent
// loop returns a non-nil, freshly rewritten history together with a NIL
// *agent.Result: every error return in the run loop yields `history, nil, err`,
// and Result.HistoryRewritten is stamped by a defer that only fires on a
// non-nil result. The core persists that history anyway (failed turns are
// persisted deliberately), so without the res == nil rule the OLD boundaries
// survive on top of the rewritten array.
type compactThenFailLLM struct {
	compactionCalls int
	failures        int
}

func (l *compactThenFailLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (l *compactThenFailLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	for _, m := range req.Messages {
		if m.Role == llm.RoleSystem && strings.Contains(m.Content, "Context Manager") {
			l.compactionCalls++
			return &llm.CompleteResponse{Message: llm.Message{
				Role:    llm.RoleAssistant,
				Content: "short summary",
			}}, nil
		}
	}
	l.failures++
	return nil, errors.New("llm died right after the rewrite")
}

// A turn that rewrites history and then FAILS must clear the boundaries too.
// The core persists a failed turn's history on purpose (session_rpc.go:500-503),
// so the rewritten array lands in the session while the *agent.Result that
// would have carried HistoryRewritten is nil. Keeping the boundaries there is
// the same silent corruption as after a successful rewrite: fork hands back a
// wrong branch and rewind — destructive and persisted — discards history a
// correct cut would have kept.
func TestSessionMessage_ClearsTurnBoundaries_WhenARewritingTurnThenFails(t *testing.T) {
	root := t.TempDir()
	client := &compactThenFailLLM{}
	c := setupRewriteCore(t, root, client, 30)

	started, err := c.SessionStart(SessionStartParams{})
	if err != nil {
		t.Fatal(err)
	}
	sid := started.SessionID
	seedPriorTurn(t, c, root, sid, 12, 500)

	if _, err := c.SessionMessage(context.Background(), SessionMessageParams{
		SessionID: sid,
		Content:   "second turn",
	}); err == nil {
		t.Fatal("the turn was supposed to fail after the rewrite")
	}

	if client.compactionCalls == 0 {
		t.Fatal("compaction never fired during the turn; this test is not exercising the path it claims to")
	}
	if client.failures == 0 {
		t.Fatal("the turn never reached the failing step; this test is not exercising the path it claims to")
	}

	// Read the boundaries back off DISK, not just out of the in-memory session:
	// the corruption that matters is the one a later fork/rewind loads.
	snap, err := sessionfile.Load(root, sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.TurnStarts) != 0 {
		t.Fatalf("persisted TurnStarts = %v, want none — the failed turn still rewrote history, "+
			"so every recorded index is stale and fork/rewind must refuse instead of cutting at it", snap.TurnStarts)
	}
	if got := turnStartsOf(t, c, root, sid); len(got) != 0 {
		t.Fatalf("in-memory TurnStarts = %v, want none", got)
	}
}

// The mirror image: an ordinary append-only turn must KEEP its boundaries.
// Without this, "clear them always" would pass the test above while quietly
// disabling fork for every session.
func TestSessionMessage_KeepsTurnBoundaries_WhenHistoryWasNotRewritten(t *testing.T) {
	root := t.TempDir()
	client := &compactingLLM{}
	c := setupRewriteCore(t, root, client, -1) // compaction disabled

	started, err := c.SessionStart(SessionStartParams{})
	if err != nil {
		t.Fatal(err)
	}
	sid := started.SessionID
	n := seedPriorTurn(t, c, root, sid, 3, 20)

	if _, err := c.SessionMessage(context.Background(), SessionMessageParams{
		SessionID: sid,
		Content:   "second turn",
	}); err != nil {
		t.Fatalf("session.message: %v", err)
	}

	if client.compactionCalls != 0 {
		t.Fatalf("compaction fired %d time(s) with compaction disabled", client.compactionCalls)
	}
	got := turnStartsOf(t, c, root, sid)
	if len(got) != 2 || got[0] != 0 || got[1] != n {
		t.Fatalf("TurnStarts = %v, want [0 %d] — an append-only turn keeps every boundary, "+
			"otherwise fork is silently dead for normal sessions", got, n)
	}
}
