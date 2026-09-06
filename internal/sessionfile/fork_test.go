package sessionfile

import (
	"strings"
	"testing"

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
		Todos:    []TodoItem{{Content: "left over"}},
		CostUSD:  1.25,
		MsgCount: 5,
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

func TestForkSnapshot_RefusesWhenHistoryCannotBeMapped(t *testing.T) {
	// A compacted session: history was rewritten into a summary, so the UI's
	// user-turn count no longer has a counterpart in history. Rewind's fallback
	// is to keep the whole history; for a fork that would produce a "branch"
	// still containing everything it was meant to branch away from.
	src := forkFixture()
	src.History = []llm.Message{{Role: llm.RoleAssistant, Content: "summary of earlier turns"}}

	_, err := ForkSnapshot(src, 2, "20260906T120000-bbbb")
	if err == nil {
		t.Fatal("a session whose history cannot be mapped must be refused, not silently mis-cut")
	}
	if !strings.Contains(err.Error(), "compact") {
		t.Errorf("error should name compaction as the cause, got: %v", err)
	}
}

func TestForkSnapshot_RequiresANewID(t *testing.T) {
	if _, err := ForkSnapshot(forkFixture(), 2, ""); err == nil {
		t.Fatal("an empty new id must be refused")
	}
}
