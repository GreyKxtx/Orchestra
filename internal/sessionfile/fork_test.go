package sessionfile

import (
	"strings"
	"testing"
	"time"

	"github.com/orchestra/orchestra/llm"
)

// forkFixture builds a session with three user turns, each answered, so that
// the exclusive fork boundary is observable: forking at user turn 2 must keep
// the assistant's reply to turn 1.
func forkFixture() *Snapshot {
	return &Snapshot{
		Version: Version,
		ID:      "20260905T101500-aaaa",
		Title:   "original task",
		Model:   "test-model",
		UIMessages: []UIMessage{
			{Role: "user", Text: "u1"},
			{Role: "assistant", Text: "a1"},
			{Role: "user", Text: "u2"},
			{Role: "assistant", Text: "a2"},
			{Role: "user", Text: "u3"},
		},
		History: []llm.Message{
			{Role: llm.RoleUser, Content: "u1"},
			{Role: llm.RoleAssistant, Content: "a1"},
			{Role: llm.RoleUser, Content: "u2"},
			{Role: llm.RoleAssistant, Content: "a2"},
			{Role: llm.RoleUser, Content: "u3"},
		},
		Todos:      []TodoItem{{Content: "left over"}},
		CostUSD:    1.25,
		MsgCount:   5,
		TurnStarts: []int{0, 2, 4},
	}
}

// realisticFixture is shaped the way the product actually writes sessions.
//
// internal/agent/agent_step.go builds a fresh `system + user + history` slice
// per request, so the user's prompt is NEVER appended to the persisted
// History: history holds assistant steps and tool results only. The agent does
// inject synthetic role=user messages mid-run (LSP hints, retries, image
// carriers) — one is included here — so counting user messages in history maps
// onto nothing the UI can see.
//
// Three turns:
//
//	turn 1 (u1) -> history[0:3]  assistant tool call, tool result, assistant text
//	turn 2 (u2) -> history[3:5]  a synthetic user hint, then assistant text
//	turn 3 (u3) -> history[5:6]  assistant text
func realisticFixture() *Snapshot {
	return &Snapshot{
		Version: Version,
		ID:      "20260905T101500-real",
		Title:   "real session",
		Model:   "test-model",
		UIMessages: []UIMessage{
			{Role: "user", Text: "u1"},
			{Role: "assistant", Text: "a1"},
			{Role: "user", Text: "u2"},
			{Role: "assistant", Text: "a2"},
			{Role: "user", Text: "u3"},
			{Role: "assistant", Text: "a3"},
		},
		History: []llm.Message{
			{Role: llm.RoleAssistant, Content: "calling read_file"},
			{Role: llm.RoleTool, Content: "file contents"},
			{Role: llm.RoleAssistant, Content: "a1"},
			{Role: llm.RoleUser, Content: "[lsp] 2 diagnostics in main.go"}, // synthetic, mid-turn
			{Role: llm.RoleAssistant, Content: "a2"},
			{Role: llm.RoleAssistant, Content: "a3"},
		},
		TurnStarts: []int{0, 3, 5},
		MsgCount:   6,
	}
}

// TestForkSnapshot_CutsARealisticHistoryAtTheRecordedBoundary is the test the
// original design could not pass: a history with no ordinary user turns in it
// at all. Counting user messages returns -1 here (there is exactly one
// role=user entry and it is synthetic), so the previous implementation refused
// every real session; where a synthetic hint did make the count resolve, it
// cut mid-turn and produced a silently corrupt branch.
func TestForkSnapshot_CutsARealisticHistoryAtTheRecordedBoundary(t *testing.T) {
	src := realisticFixture()

	// Fork at u2 (index 2): the branch keeps turn 1 whole — tool call, tool
	// result and the assistant's answer — and nothing of turn 2.
	got, err := ForkSnapshot(src, 2, "20260906T120000-bbbb")
	if err != nil {
		t.Fatalf("ForkSnapshot on a realistically shaped history: %v", err)
	}
	if len(got.History) != 3 {
		t.Fatalf("History = %d (%+v), want 3 — the whole of turn 1", len(got.History), got.History)
	}
	if got.History[2].Content != "a1" {
		t.Errorf("branch must end with turn 1's assistant answer, got %q", got.History[2].Content)
	}
	if len(got.UIMessages) != 2 {
		t.Errorf("UIMessages = %d, want 2", len(got.UIMessages))
	}

	// Fork at u3 (index 4): turn 2 survives whole, including the synthetic
	// role=user hint that sits inside it.
	got, err = ForkSnapshot(src, 4, "20260906T120000-cccc")
	if err != nil {
		t.Fatalf("ForkSnapshot at u3: %v", err)
	}
	if len(got.History) != 5 {
		t.Fatalf("History = %d (%+v), want 5 — turns 1 and 2 whole", len(got.History), got.History)
	}
	if got.History[3].Role != llm.RoleUser {
		t.Errorf("the synthetic mid-turn hint must survive inside its own turn, got %+v", got.History[3])
	}
}

// TestForkSnapshot_BranchKeepsItsOwnBoundaries: a branch must itself be
// forkable, so the retained boundaries travel with the retained history.
func TestForkSnapshot_BranchKeepsItsOwnBoundaries(t *testing.T) {
	got, err := ForkSnapshot(realisticFixture(), 4, "20260906T120000-bbbb")
	if err != nil {
		t.Fatal(err)
	}
	want := []int{0, 3}
	if len(got.TurnStarts) != len(want) {
		t.Fatalf("TurnStarts = %v, want %v", got.TurnStarts, want)
	}
	for i := range want {
		if got.TurnStarts[i] != want[i] {
			t.Fatalf("TurnStarts = %v, want %v", got.TurnStarts, want)
		}
	}
	if _, err := ForkSnapshot(got, 2, "20260906T120000-dddd"); err != nil {
		t.Fatalf("a branch must itself be forkable: %v", err)
	}
}

// TestForkSnapshot_BranchGetsItsOwnCreationTime (M1): Save only stamps
// CreatedAt when it is zero, so copying the parent's value made every branch
// report its parent's creation time.
func TestForkSnapshot_BranchGetsItsOwnCreationTime(t *testing.T) {
	src := forkFixture()
	src.CreatedAt = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	got, err := ForkSnapshot(src, 2, "20260906T120000-bbbb")
	if err != nil {
		t.Fatal(err)
	}
	if !got.CreatedAt.IsZero() {
		t.Fatalf("CreatedAt = %v, want zero so Save stamps the branch's own creation time", got.CreatedAt)
	}
}

// TestForkSnapshot_ForkingAForkDoesNotStackTheSuffix (M2).
func TestForkSnapshot_ForkingAForkDoesNotStackTheSuffix(t *testing.T) {
	src := forkFixture()
	src.Title = "original task (fork)"

	got, err := ForkSnapshot(src, 2, "20260906T120000-bbbb")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "original task (fork)" {
		t.Fatalf("Title = %q, want the suffix not stacked", got.Title)
	}
}

func TestForkSnapshot_ExcludesTheForkPointAndKeepsThePreviousReply(t *testing.T) {
	src := forkFixture()

	got, err := ForkSnapshot(src, 2, "20260906T120000-bbbb")
	if err != nil {
		t.Fatalf("ForkSnapshot: %v", err)
	}

	// Forking at u2 means "try turn 2 differently": the branch ends with a1,
	// so the next thing written into it is a fresh prompt rather than a second
	// user message in a row.
	if len(got.UIMessages) != 2 {
		t.Fatalf("UIMessages = %d, want 2", len(got.UIMessages))
	}
	if got.UIMessages[1].Text != "a1" {
		t.Fatalf("branch must end with the previous assistant reply, got %q", got.UIMessages[1].Text)
	}
	if len(got.History) != 2 {
		t.Fatalf("History = %d, want 2 (u1, a1)", len(got.History))
	}
	if got.History[1].Content != "a1" {
		t.Fatalf("history must keep the assistant reply, got %q", got.History[1].Content)
	}
}

func TestForkSnapshot_RecordsLineageAndResetsAbandonedState(t *testing.T) {
	src := forkFixture()

	got, err := ForkSnapshot(src, 2, "20260906T120000-bbbb")
	if err != nil {
		t.Fatal(err)
	}

	if got.ID != "20260906T120000-bbbb" {
		t.Errorf("ID = %q", got.ID)
	}
	if got.ParentID != src.ID {
		t.Errorf("ParentID = %q, want %q", got.ParentID, src.ID)
	}
	if got.ForkedFromIndex != 2 {
		t.Errorf("ForkedFromIndex = %d, want 2", got.ForkedFromIndex)
	}
	if !strings.HasSuffix(got.Title, " (fork)") {
		t.Errorf("Title = %q, want the parent title plus a fork marker", got.Title)
	}
	// Todos, pending ops, spend and apply output all describe the abandoned
	// path; carrying them into the branch would double-count the spend and
	// show work the branch does not contain.
	if got.Todos != nil {
		t.Errorf("Todos = %+v, want nil", got.Todos)
	}
	if got.PendingOps != nil {
		t.Errorf("PendingOps = %+v, want nil", got.PendingOps)
	}
	if got.CostUSD != 0 {
		t.Errorf("CostUSD = %v, want 0", got.CostUSD)
	}
	if got.MsgCount != 2 {
		t.Errorf("MsgCount = %d, want 2", got.MsgCount)
	}
	if got.Version != Version {
		t.Errorf("Version = %d, want %d — the schema is not bumped by forking", got.Version, Version)
	}
}

func TestForkSnapshot_LeavesTheParentUntouched(t *testing.T) {
	src := forkFixture()

	if _, err := ForkSnapshot(src, 2, "20260906T120000-bbbb"); err != nil {
		t.Fatal(err)
	}

	// The whole point of fork over rewind is that the original survives.
	want := forkFixture()
	if len(src.UIMessages) != len(want.UIMessages) || len(src.History) != len(want.History) {
		t.Fatalf("parent was mutated: ui=%d hist=%d", len(src.UIMessages), len(src.History))
	}
	if src.ID != want.ID || src.Title != want.Title || src.CostUSD != want.CostUSD {
		t.Fatalf("parent metadata was mutated: %+v", src)
	}
	if len(src.Todos) != 1 {
		t.Fatalf("parent todos were cleared: %+v", src.Todos)
	}
}

func TestForkSnapshot_RefusesIndexZero(t *testing.T) {
	_, err := ForkSnapshot(forkFixture(), 0, "20260906T120000-bbbb")
	if err == nil {
		t.Fatal("forking at the first message must be refused — the branch would be empty")
	}
}

func TestForkSnapshot_RefusesNonUserAndOutOfRangeIndexes(t *testing.T) {
	if _, err := ForkSnapshot(forkFixture(), 1, "x"); err == nil {
		t.Error("an assistant message is not a checkpoint")
	} else if !strings.Contains(err.Error(), "assistant") {
		t.Errorf("error should name the actual role, got: %v", err)
	}
	if _, err := ForkSnapshot(forkFixture(), 99, "x"); err == nil {
		t.Error("out-of-range index must be refused")
	}
	if _, err := ForkSnapshot(forkFixture(), -1, "x"); err == nil {
		t.Error("negative index must be refused")
	}
}

func TestForkSnapshot_RefusesWhenThereIsNoRecordedBoundary(t *testing.T) {
	// Two ways to get here, and the error must not misdiagnose either: the
	// session predates turn-boundary recording, or /compact rewrote history
	// wholesale and SessionCompact cleared the boundaries. Rewind's fallback
	// is to keep the whole history; for a fork that would produce a "branch"
	// still containing everything it was meant to branch away from.
	src := forkFixture()
	src.TurnStarts = nil

	_, err := ForkSnapshot(src, 2, "20260906T120000-bbbb")
	if err == nil {
		t.Fatal("a session with no recorded boundary must be refused, not silently mis-cut")
	}
	if !strings.Contains(err.Error(), "turn boundaries") {
		t.Errorf("error should say the session has no recorded turn boundaries, got: %v", err)
	}

	// Boundaries recorded, but not for the requested turn (the session was
	// recorded from partway through its life).
	short := forkFixture()
	short.TurnStarts = []int{0}
	if _, err := ForkSnapshot(short, 2, "20260906T120000-bbbb"); err == nil {
		t.Fatal("a turn with no recorded boundary must be refused")
	}
}

// TestForkSnapshot_RefusesABoundaryOutsideHistory guards against a stale
// boundary array pointing past a shortened history.
func TestForkSnapshot_RefusesABoundaryOutsideHistory(t *testing.T) {
	src := forkFixture()
	src.TurnStarts = []int{0, 99, 100}

	if _, err := ForkSnapshot(src, 2, "20260906T120000-bbbb"); err == nil {
		t.Fatal("a boundary pointing past the end of history must be refused")
	}
}

func TestForkSnapshot_RequiresANewID(t *testing.T) {
	if _, err := ForkSnapshot(forkFixture(), 2, ""); err == nil {
		t.Fatal("an empty new id must be refused")
	}
}
