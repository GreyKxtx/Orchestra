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
	// A 4 KB history budget with a 30% trigger fires compaction on a history
	// of a few thousand bytes, without needing a real long session.
	return setupRewriteCoreWithBudget(t, root, client, compactPct, 4)
}

// setupRewriteCoreWithBudget is setupRewriteCore with the context budget under
// test control. The trigger is not purely a function of history size: the
// estimator adds a fixed ~32 KB stand-in for system prompt and tool schemas
// (agent/context_estimate.go), so at a 4 KB budget EVERY turn with any history
// at all compacts. A test that needs an ordinary append-only turn to follow a
// compacted one has to give the budget enough room for that.
func setupRewriteCoreWithBudget(t *testing.T, root string, client llm.Client, compactPct, contextKB int) *Core {
	t.Helper()
	cfg := config.DefaultConfig(root)
	lspOff := false
	cfg.LSP.Enabled = &lspOff
	cfg.LSP.AutoInstall = "false"
	cfg.Limits.ContextKB = contextKB
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

// wantAllUnknown asserts that every entry is the unknown sentinel and that the
// array kept its length — the length is the alignment with the UI's user
// turns, and losing it is what used to end forking for a whole session.
func wantAllUnknown(t *testing.T, got []int, wantLen int, why string) {
	t.Helper()
	if len(got) != wantLen {
		t.Fatalf("TurnStarts = %v, want %d entries — %s; the array must keep one slot per user turn, "+
			"or every later turn's lookup lands past its end and fork is dead for this session forever",
			got, wantLen, why)
	}
	for i, v := range got {
		if v != sessionfile.TurnStartUnknown {
			t.Fatalf("TurnStarts = %v: entry %d = %d, want the unknown sentinel — %s", got, i, v, why)
		}
	}
}

// A turn whose run rewrote history must leave every recorded boundary MARKED
// unknown — not deleted. The rewrite (compaction, or the truncation fallback)
// replaces the whole array, so no index recorded against the old one survives;
// but the slots must stay, so the turns recorded afterwards line up again.
func TestSessionMessage_MarksTurnBoundariesUnknown_WhenTheTurnRewroteHistory(t *testing.T) {
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
	// Two slots: the seeded first turn, and the turn just run. Both boundaries
	// were recorded against the array the rewrite threw away.
	wantAllUnknown(t, turnStartsOf(t, c, root, sid), 2,
		"the turn rewrote history, so every recorded index is stale; fork would cut a wrong branch "+
			"and rewind would discard history a correct cut keeps")
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

// A turn that rewrites history and then FAILS must mark the boundaries unknown
// too. The core persists a failed turn's history on purpose (session_rpc.go),
// so the rewritten array lands in the session while the *agent.Result that
// would have carried HistoryRewritten is nil. Leaving the boundaries usable is
// the same silent corruption as after a successful rewrite: fork hands back a
// wrong branch and rewind — destructive and persisted — discards history a
// correct cut would have kept.
func TestSessionMessage_MarksTurnBoundariesUnknown_WhenARewritingTurnThenFails(t *testing.T) {
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
	wantAllUnknown(t, snap.TurnStarts, 2,
		"the failed turn still rewrote history, so every recorded index is stale and fork/rewind "+
			"must refuse instead of cutting at it — but the slots must survive on disk")
	wantAllUnknown(t, turnStartsOf(t, c, root, sid), 2, "in-memory boundaries must match the persisted ones")
}

// stepScriptLLM answers compaction requests with a short summary (so the
// convergence guard adopts it) and every ordinary step with a final answer —
// except on the turn whose prompt contains failOnPrompt, which errors out.
// Keyed on the prompt rather than on a step number because a turn is not
// guaranteed to be one step, and a miscount would silently move the failure
// to a different turn than the test describes.
type stepScriptLLM struct {
	failOnPrompt    string
	compactionCalls int
	failures        int
}

func (l *stepScriptLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (l *stepScriptLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	for _, m := range req.Messages {
		if m.Role == llm.RoleSystem && strings.Contains(m.Content, "Context Manager") {
			l.compactionCalls++
			return &llm.CompleteResponse{Message: llm.Message{
				Role:    llm.RoleAssistant,
				Content: "short summary",
			}}, nil
		}
	}
	// agent_step.go builds `system + user + history`, so the FIRST user
	// message is this turn's prompt. Later user-role entries are the synthetic
	// ones the agent injects into history and must not be matched.
	for _, m := range req.Messages {
		if m.Role != llm.RoleUser {
			continue
		}
		if l.failOnPrompt != "" && strings.Contains(m.Content, l.failOnPrompt) {
			l.failures++
			return nil, errors.New("llm died right after the rewrite")
		}
		break
	}
	return &llm.CompleteResponse{Message: llm.Message{
		Role:    llm.RoleAssistant,
		Content: `{"type":"final","final":{"patches":[]}}`,
	}}, nil
}

// THE POINT OF THE WHOLE CHANGE. One interrupted turn that rewrote history
// used to end forking for the session permanently: the boundaries were
// cleared, so the array stopped growing in step with the UI's user turns and
// every later lookup landed past its end. Marking keeps the array aligned, so
// a turn recorded AFTER the rewrite is forkable again — while forking AT the
// rewritten turn still refuses, naming that turn.
func TestSessionMessage_AnInterruptedRewritingTurnKeepsLaterTurnsForkable(t *testing.T) {
	root := t.TempDir()
	client := &stepScriptLLM{failOnPrompt: "second"} // turn 1 ok, turn 2 dies, turn 3 ok
	// 256 KB budget: a ~50 KB history compacts, and the ~8 KB summary+tail it
	// leaves behind does not — so turn 3 is a genuine append-only turn.
	c := setupRewriteCoreWithBudget(t, root, client, 30, 256)

	started, err := c.SessionStart(SessionStartParams{})
	if err != nil {
		t.Fatal(err)
	}
	sid := started.SessionID

	// Turn 1: an ordinary append-only turn, through the real path, so the UI
	// projection and the boundary array both get their first entry.
	if _, err := c.SessionMessage(context.Background(), SessionMessageParams{SessionID: sid, Content: "first"}); err != nil {
		t.Fatalf("turn 1: %v", err)
	}

	// Give the session enough history that turn 2 trips the compaction
	// threshold, without disturbing turn 1's boundary — turn 1 still starts at
	// index 0.
	sess, err := c.sessions.GetOrLoad(root, sid)
	if err != nil {
		t.Fatal(err)
	}
	// Tool-bearing atoms on purpose: splitHistoryForCompaction keeps growing
	// the verbatim tail until it has minToolAtoms tool atoms, so an
	// all-assistant history leaves nothing to summarise, the convergence guard
	// rejects the result and the run falls through to truncation instead.
	big := make([]llm.Message, 0, 100)
	for i := 0; i < 50; i++ {
		big = append(big,
			llm.Message{Role: llm.RoleAssistant, Content: strings.Repeat("x", 500)},
			llm.Message{Role: llm.RoleTool, Content: strings.Repeat("y", 500)},
		)
	}
	sess.Lock()
	sess.ReplaceHistory(big)
	err = sess.Snapshot(root)
	sess.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	// Turn 2: compacts, then dies. This is the Ctrl+C case.
	if _, err := c.SessionMessage(context.Background(), SessionMessageParams{SessionID: sid, Content: "second"}); err == nil {
		t.Fatal("turn 2 was supposed to fail after the rewrite")
	}
	if client.compactionCalls != 1 || client.failures != 1 {
		t.Fatalf("turn 2 did not exercise rewrite-then-fail (compactions=%d failures=%d)",
			client.compactionCalls, client.failures)
	}

	// Turn 3: an ordinary turn on the compacted history. It must record a REAL
	// boundary in slot 2, even though slots 0 and 1 are unknown.
	if _, err := c.SessionMessage(context.Background(), SessionMessageParams{SessionID: sid, Content: "third"}); err != nil {
		t.Fatalf("turn 3: %v", err)
	}
	if client.compactionCalls != 1 {
		t.Fatalf("turn 3 compacted again (compactions=%d); it is no longer the append-only turn "+
			"this test needs", client.compactionCalls)
	}

	got := turnStartsOf(t, c, root, sid)
	if len(got) != 3 {
		t.Fatalf("TurnStarts = %v, want 3 entries — one per user turn; clearing is what broke this", got)
	}
	if got[0] != sessionfile.TurnStartUnknown || got[1] != sessionfile.TurnStartUnknown {
		t.Fatalf("TurnStarts = %v: the turns the rewrite ran under must be marked unknown", got)
	}
	if got[2] == sessionfile.TurnStartUnknown {
		t.Fatalf("TurnStarts = %v: turn 3 ran after the rewrite and must record a real boundary", got)
	}

	// Forking at turn 3's prompt (ui index 2 — core appends only the user
	// messages) uses that real boundary and must succeed.
	branch, err := c.SessionFork(SessionForkParams{SessionID: sid, UIMessageIndex: 2})
	if err != nil {
		t.Fatalf("forking at a turn recorded after the rewrite must work; this is the whole point "+
			"of marking instead of clearing: %v", err)
	}
	if branch.HistoryMessages != got[2] {
		t.Errorf("branch history = %d, want %d (turn 3's recorded boundary)", branch.HistoryMessages, got[2])
	}

	// Forking AT the rewritten turn must still refuse, and say why.
	_, err = c.SessionFork(SessionForkParams{SessionID: sid, UIMessageIndex: 1})
	if err == nil {
		t.Fatal("forking at the rewritten turn must be refused, not silently mis-cut")
	}
	if !strings.Contains(err.Error(), "turn 2") || !strings.Contains(err.Error(), "unknown") {
		t.Errorf("the refusal must name turn 2's boundary as unknown, got: %v", err)
	}
}

// A mid-turn rewrite invalidates the EARLIER turns' boundaries, not only the
// running turn's — and the bounds check does not catch it. compactHistory
// replaces the array with a summary plus a tail, so an earlier boundary that
// happens to be smaller than the compacted length still "resolves", straight
// into the summary. That is the silently wrong branch the whole boundary
// design exists to prevent, so marking only the current turn is not enough.
//
// The fixture makes the hole explicit: turn 2's boundary (10) stays inside the
// post-compaction history, so nothing but the unknown marker can refuse it.
func TestSessionMessage_ARewritingTurnInvalidatesTheEARLIERBoundariesToo(t *testing.T) {
	root := t.TempDir()
	client := &stepScriptLLM{failOnPrompt: "third"} // turns 1 and 2 succeed, turn 3 rewrites and dies
	c := setupRewriteCoreWithBudget(t, root, client, 30, 256)

	started, err := c.SessionStart(SessionStartParams{})
	if err != nil {
		t.Fatal(err)
	}
	sid := started.SessionID
	sess, err := c.sessions.GetOrLoad(root, sid)
	if err != nil {
		t.Fatal(err)
	}
	setHistory := func(msgs []llm.Message) {
		t.Helper()
		sess.Lock()
		sess.ReplaceHistory(msgs)
		snapErr := sess.Snapshot(root)
		sess.Unlock()
		if snapErr != nil {
			t.Fatal(snapErr)
		}
	}

	if _, err := c.SessionMessage(context.Background(), SessionMessageParams{SessionID: sid, Content: "first"}); err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	// A short history, so turn 2's boundary is a SMALL index — small enough to
	// stay in range after the compaction that is about to happen.
	small := make([]llm.Message, 0, 10)
	for i := 0; i < 10; i++ {
		small = append(small, llm.Message{Role: llm.RoleAssistant, Content: "a"})
	}
	setHistory(small)

	if _, err := c.SessionMessage(context.Background(), SessionMessageParams{SessionID: sid, Content: "second"}); err != nil {
		t.Fatalf("turn 2: %v", err)
	}
	if got := turnStartsOf(t, c, root, sid); len(got) != 2 || got[1] != 10 {
		t.Fatalf("TurnStarts before the rewrite = %v, want [0 10]", got)
	}

	// Now a big, tool-bearing history so turn 3 actually compacts.
	big := make([]llm.Message, 0, 100)
	for i := 0; i < 50; i++ {
		big = append(big,
			llm.Message{Role: llm.RoleAssistant, Content: strings.Repeat("x", 500)},
			llm.Message{Role: llm.RoleTool, Content: strings.Repeat("y", 500)},
		)
	}
	setHistory(big)

	if _, err := c.SessionMessage(context.Background(), SessionMessageParams{SessionID: sid, Content: "third"}); err == nil {
		t.Fatal("turn 3 was supposed to fail after the rewrite")
	}
	if client.compactionCalls != 1 || client.failures != 1 {
		t.Fatalf("turn 3 did not exercise rewrite-then-fail (compactions=%d failures=%d)",
			client.compactionCalls, client.failures)
	}

	sess.Lock()
	histLen := len(sess.CopyHistory())
	sess.Unlock()
	if histLen >= len(big) || histLen <= 10 {
		t.Fatalf("post-rewrite history = %d messages, want a compacted array longer than turn 2's "+
			"boundary (10) and shorter than the %d it replaced — otherwise the bounds check would "+
			"catch the stale entry on its own and this test proves nothing", histLen, len(big))
	}

	// Turn 2's boundary is in range and would cut. Only the marker refuses it.
	if _, err := c.SessionFork(SessionForkParams{SessionID: sid, UIMessageIndex: 1}); err == nil {
		t.Fatal("forking at turn 2 after a mid-turn compaction must be refused: its recorded boundary " +
			"now points into a summary it knows nothing about, and the cut would be silently wrong")
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
