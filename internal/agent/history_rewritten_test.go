package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// Result.HistoryRewritten is the only signal the core has that the indices it
// recorded into the history array (Snapshot.TurnStarts, which drive
// session.fork and session.rewind) are stale. The agent rewrites history in
// TWO places mid-turn — compaction adopting a summary, and the truncation
// fallback when compaction errors or refuses to converge. Both must raise it;
// a run that only appended must not.

// rewriteLLM answers compaction requests with a scripted summary and every
// other request with a final step, so a run is exactly one step long.
type rewriteLLM struct {
	// compactSummary is returned for the compaction request. A short one
	// shrinks history past the 20% convergence guard and gets adopted; a long
	// one makes the guard refuse it and the loop fall back to truncation.
	compactSummary string
	// compactErr, when set, makes the compaction request fail instead.
	compactErr      error
	compactionCalls int
}

func (l *rewriteLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (l *rewriteLLM) Complete(_ context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	for _, m := range req.Messages {
		if m.Role == llm.RoleSystem && strings.Contains(m.Content, "Context Manager") {
			l.compactionCalls++
			if l.compactErr != nil {
				return nil, l.compactErr
			}
			return &llm.CompleteResponse{Message: llm.Message{
				Role:    llm.RoleAssistant,
				Content: l.compactSummary,
			}}, nil
		}
	}
	return &llm.CompleteResponse{Message: llm.Message{
		Role:    llm.RoleAssistant,
		Content: `{"type":"final","final":{"patches":[]}}`,
	}}, nil
}

// bulkyHistory is big enough that compaction has an older half to summarise.
// The atoms have to be TOOL-bearing: splitHistoryForCompaction keeps growing
// the verbatim tail until it holds minToolAtoms tool atoms, so a history of
// plain assistant messages leaves nothing older to summarise and every run
// lands in the truncation fallback instead of the adoption branch.
func bulkyHistory() []llm.Message {
	out := make([]llm.Message, 0, 24)
	for i := 0; i < 12; i++ {
		id := "call-" + string(rune('a'+i))
		out = append(out,
			llm.Message{
				Role: llm.RoleAssistant,
				ToolCalls: []llm.ToolCall{{
					ID:       id,
					Type:     "function",
					Function: llm.ToolCallFunc{Name: "ls", Arguments: llm.ToolArguments([]byte(`{"path":"."}`))},
				}},
			},
			llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: id,
				Content:    strings.Repeat("history atom ", 40),
			},
		)
	}
	return out
}

func runWithForcedCompaction(t *testing.T, client llm.Client) (*Result, []llm.Message) {
	t.Helper()
	dir := t.TempDir()
	runner, err := tools.NewRunner(dir, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	ag, err := New(client, v, runner, Options{
		MaxSteps:            4,
		MaxPromptBytes:      4000,
		CompactThresholdPct: 60,
		ForceCompactOnce:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	hist, res, err := ag.Run(context.Background(), bulkyHistory(), "keep going")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res == nil {
		t.Fatal("Run returned a nil result")
	}
	return res, hist
}

// historyHas reports whether any message carries marker — used to pin which
// rewrite branch a run actually took, so a test cannot quietly drift onto the
// other one and keep passing for the wrong reason.
func historyHas(hist []llm.Message, marker string) bool {
	for _, m := range hist {
		if strings.Contains(m.Content, marker) {
			return true
		}
	}
	return false
}

// The compaction success path: the summary shrank history, so it was adopted
// and every recorded index into the old array is meaningless.
func TestRun_ReportsHistoryRewritten_WhenCompactionIsAdopted(t *testing.T) {
	client := &rewriteLLM{compactSummary: "short summary"}
	res, hist := runWithForcedCompaction(t, client)

	if client.compactionCalls == 0 {
		t.Fatal("compaction never fired; the test is not exercising the path it claims to")
	}
	if !historyHas(hist, checkpointHeader) {
		t.Fatalf("the summary was not adopted — this run took the truncation fallback, "+
			"not the compaction branch this test covers (history=%d msgs)", len(hist))
	}
	if !res.HistoryRewritten {
		t.Fatal("HistoryRewritten = false after compaction replaced history — " +
			"the core will keep turn boundaries that now index a rewritten array")
	}
}

// The branch most likely to be missed: compaction ran but the convergence
// guard refused its result, so the loop replaced history with
// truncateMessages instead. Not compaction — but just as destructive to
// recorded indices, so the flag must be raised here too.
func TestRun_ReportsHistoryRewritten_WhenCompactionDeclinesToConverge(t *testing.T) {
	// A "summary" as big as the input: < 20% shrink, so the guard refuses it.
	client := &rewriteLLM{compactSummary: strings.Repeat("bloated summary ", 500)}
	res, hist := runWithForcedCompaction(t, client)

	if client.compactionCalls == 0 {
		t.Fatal("compaction never fired; the test is not exercising the path it claims to")
	}
	if historyHas(hist, checkpointHeader) {
		t.Fatal("the bloated summary was adopted — the convergence guard did not refuse it, " +
			"so this test is covering the compaction branch, not the fallback it claims")
	}
	if !res.HistoryRewritten {
		t.Fatal("HistoryRewritten = false after the truncation fallback replaced history — " +
			"this branch is not compaction, but it invalidates history indices just the same")
	}
}

// Compaction erroring is the other way into the same truncation fallback.
func TestRun_ReportsHistoryRewritten_WhenCompactionFails(t *testing.T) {
	client := &rewriteLLM{compactErr: errCompactBoom}
	res, hist := runWithForcedCompaction(t, client)

	if client.compactionCalls == 0 {
		t.Fatal("compaction never fired; the test is not exercising the path it claims to")
	}
	if historyHas(hist, checkpointHeader) {
		t.Fatal("compaction was supposed to fail, yet a checkpoint summary landed in history")
	}
	if !res.HistoryRewritten {
		t.Fatal("HistoryRewritten = false after a failed compaction fell back to truncation")
	}
}

// The negative case that keeps the fix from degenerating into "always clear":
// a plain turn only appends, so the caller's indices stay valid and fork keeps
// working.
func TestRun_DoesNotReportHistoryRewritten_ForAPlainTurn(t *testing.T) {
	dir := t.TempDir()
	runner, err := tools.NewRunner(dir, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	client := &rewriteLLM{compactSummary: "short summary"}
	ag, err := New(client, v, runner, Options{
		MaxSteps:            4,
		CompactThresholdPct: -1, // compaction disabled entirely
	})
	if err != nil {
		t.Fatal(err)
	}
	_, res, err := ag.Run(context.Background(), bulkyHistory(), "hello")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res == nil {
		t.Fatal("Run returned a nil result")
	}
	if client.compactionCalls != 0 {
		t.Fatalf("compaction fired %d time(s) with compaction disabled", client.compactionCalls)
	}
	if res.HistoryRewritten {
		t.Fatal("HistoryRewritten = true for an append-only turn — " +
			"the core would drop turn boundaries that are still perfectly valid, disabling fork")
	}
}

type compactBoom struct{}

func (compactBoom) Error() string { return "compaction exploded" }

var errCompactBoom = compactBoom{}

// The Result is not enough. A turn that rewrites history at step 1 and is then
// KILLED — Ctrl+C, crash, OOM — never produces a Result at all, yet the
// five-second mid-turn snapshot has already written the rewritten array to
// disk beside the boundaries recorded for the OLD one. Those boundaries are
// still in range, so they cut silently and wrongly on the next load. The
// OnStepHistory hook is the only place that failure can be closed, so it
// carries the same invalidation signal the Result does.
//
// The flag is cumulative for the turn, exactly like Result.HistoryRewritten:
// once an array has been replaced, every later snapshot of it is a snapshot of
// the new array.
type rewriteThenToolLLM struct {
	rewriteLLM
	stepCalls int
}

func (l *rewriteThenToolLLM) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	for _, m := range req.Messages {
		if m.Role == llm.RoleSystem && strings.Contains(m.Content, "Context Manager") {
			return l.rewriteLLM.Complete(ctx, req)
		}
	}
	l.stepCalls++
	if l.stepCalls == 1 {
		// One tool step, so the mid-turn hook actually fires.
		return &llm.CompleteResponse{Message: llm.Message{
			Role:    llm.RoleAssistant,
			Content: `{"type":"tool_call","tool":{"name":"bash","input":{"command":"echo","args":["hello"]}}}`,
		}}, nil
	}
	return &llm.CompleteResponse{Message: llm.Message{
		Role:    llm.RoleAssistant,
		Content: `{"type":"final","final":{"patches":[]}}`,
	}}, nil
}

func runWithStepHistorySink(t *testing.T, client llm.Client, opts Options) []bool {
	t.Helper()
	dir := t.TempDir()
	runner, err := tools.NewRunner(dir, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })

	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	var seen []bool
	opts.OnStepHistory = func(_ int, _ []llm.Message, rewritten bool) {
		seen = append(seen, rewritten)
	}
	ag, err := New(client, v, runner, opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ag.Run(context.Background(), bulkyHistory(), "keep going"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(seen) == 0 {
		t.Fatal("OnStepHistory never fired; the run took no tool step and proves nothing")
	}
	return seen
}

func TestOnStepHistory_ReportsHistoryRewritten_AfterAMidTurnCompaction(t *testing.T) {
	client := &rewriteThenToolLLM{rewriteLLM: rewriteLLM{compactSummary: "short summary"}}
	seen := runWithStepHistorySink(t, client, Options{
		MaxSteps:            4,
		MaxPromptBytes:      4000,
		CompactThresholdPct: 60,
		ForceCompactOnce:    true,
	})
	if client.compactionCalls == 0 {
		t.Fatal("compaction never ran; the test would pass for the wrong reason")
	}
	for i, rewritten := range seen {
		if !rewritten {
			t.Fatalf("OnStepHistory call %d reported rewritten=false after compaction replaced "+
				"history — the mid-turn snapshot persists the new array under the old turn "+
				"boundaries, and a kill at that moment leaves fork cutting at a stale index", i)
		}
	}
}

func TestOnStepHistory_DoesNotReportHistoryRewritten_ForAPlainTurn(t *testing.T) {
	client := &rewriteThenToolLLM{rewriteLLM: rewriteLLM{compactSummary: "short summary"}}
	seen := runWithStepHistorySink(t, client, Options{
		MaxSteps:            4,
		CompactThresholdPct: -1, // compaction disabled entirely
	})
	if client.compactionCalls != 0 {
		t.Fatalf("compaction fired %d time(s) with compaction disabled", client.compactionCalls)
	}
	for i, rewritten := range seen {
		if rewritten {
			t.Fatalf("OnStepHistory call %d reported rewritten=true on an append-only turn — "+
				"the core would blank turn boundaries that are still valid, disabling fork", i)
		}
	}
}
