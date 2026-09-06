# Session Search and Fork Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add content search across saved sessions (`orchestra session search`, `/sessions <query>`) and non-destructive forking from a checkpoint (`orchestra session fork`, `/fork`, `session.fork` RPC), so a past session can be found by what was said in it and a new branch can be tried without destroying the original.

**Architecture:** All logic lives as pure functions over `sessionfile.Snapshot` in `internal/sessionfile` — `Search`, `ForkSnapshot`, and the shared history locator `IndexOfNthUserMessage`. Every surface is a thin wrapper: the CLI and TUI call those functions directly (as they already do for `ListMeta`), and two new RPC methods wrap them for clients that are not in-process. `session.rewind` is refactored onto the shared locator with its observable behaviour unchanged; its existing tests are the regression proof.

**Tech Stack:** Go 1.25, existing `internal/sessionfile` JSON storage (schema v4, unchanged), `github.com/spf13/cobra` for the CLI, Bubbletea for the TUI.

**Spec:** `docs/superpowers/specs/2026-09-06-session-search-fork-design.md` (commit `b8fe83e`)

## Global Constraints

- **The on-disk schema version stays at 4.** `LoadFromDisk` rejects any snapshot whose `Version` is not exactly the binary's own (`internal/core/session/persist.go:101-103`), so a bump would make files written by the new binary unreadable by an older one. Lineage fields are additive with `omitempty`.
- **No search index, no database.** `ListMeta` already reads and fully parses every session file on every call (`internal/sessionfile/store.go:74-110`); search adds work on bytes already being read, not a new class of I/O. §1.4 #4 already closed "SQLite instead of JSON" as *не делать*.
- **`session.rewind` behaviour must not change.** It stays destructive and keeps its not-found fallback (keep the full history). Only its internals move onto the shared locator.
- **Fork refuses rather than guesses.** Index 0, a non-user index, an out-of-range index, and a history that cannot be mapped (post-`/compact`) are all errors. A branch that silently still contains what it was supposed to branch away from is worse than a refusal.
- **Search defaults to case-sensitive with `-i/--insensitive`,** mirroring `orchestra search` (`internal/cli/search.go:27`), so two sibling commands do not have opposite defaults.
- **Search reads `UIMessage.Text` only by default**; `--all` adds `Reasoning` and tool blocks.
- **User-facing string languages follow the surface:** CLI help and output are English (see `internal/cli/session.go:17-19`); TUI toasts and dialog hints are Russian (see `ui/tui/app_rewind.go:21`, `:28`).
- After every task: `go build ./...`, `go vet ./...`, the full root-module suite, and `go test -race` on touched packages. `internal/core` has a known pre-existing flaky test that fails only under full-suite parallel load and passes in isolation — do not chase it.
- Every task ends in its own thematic commit.

---

## Task 1: Shared history locator

`truncateHistoryForUIPrefix` (`internal/core/session_rewind.go:81-107`) keeps `hist[:i+1]` — **inclusive** of the Nth user message. Fork needs `hist[:i]` — **exclusive**. One character apart with opposite meaning, so neither is expressed in terms of the other: both are expressed over a locator that returns a position.

**Files:**
- Create: `internal/sessionfile/history.go`
- Create: `internal/sessionfile/history_test.go`
- Modify: `internal/core/session_rewind.go:81-107`

**Interfaces:**
- Produces: `func IndexOfNthUserMessage(hist []llm.Message, n int) int`, `func CountUserMessages(ui []UIMessage) int` — both used by `ForkSnapshot` (Task 2) and by the rewritten `truncateHistoryForUIPrefix`.

- [ ] **Step 1: Write the failing test**

Create `internal/sessionfile/history_test.go`:

```go
package sessionfile

import (
	"testing"

	"github.com/orchestra/orchestra/llm"
)

func userAssistantHistory() []llm.Message {
	return []llm.Message{
		{Role: llm.RoleUser, Content: "u1"},
		{Role: llm.RoleAssistant, Content: "a1"},
		{Role: llm.RoleUser, Content: "u2"},
		{Role: llm.RoleAssistant, Content: "a2"},
		{Role: llm.RoleUser, Content: "u3"},
	}
}

func TestIndexOfNthUserMessage_FindsNth(t *testing.T) {
	hist := userAssistantHistory()
	for n, want := range map[int]int{1: 0, 2: 2, 3: 4} {
		if got := IndexOfNthUserMessage(hist, n); got != want {
			t.Errorf("IndexOfNthUserMessage(hist, %d) = %d, want %d", n, got, want)
		}
	}
}

func TestIndexOfNthUserMessage_MissingReturnsMinusOne(t *testing.T) {
	// Asking for more user messages than exist is exactly the post-compaction
	// case: fork must be able to detect it rather than cut at a wrong place.
	if got := IndexOfNthUserMessage(userAssistantHistory(), 4); got != -1 {
		t.Fatalf("got %d, want -1", got)
	}
	if got := IndexOfNthUserMessage(nil, 1); got != -1 {
		t.Fatalf("empty history: got %d, want -1", got)
	}
}

func TestIndexOfNthUserMessage_RejectsNonPositiveN(t *testing.T) {
	for _, n := range []int{0, -1} {
		if got := IndexOfNthUserMessage(userAssistantHistory(), n); got != -1 {
			t.Errorf("n=%d: got %d, want -1", n, got)
		}
	}
}

func TestCountUserMessages(t *testing.T) {
	ui := []UIMessage{
		{Role: "user"}, {Role: "assistant"}, {Role: "system"}, {Role: "user"},
	}
	if got := CountUserMessages(ui); got != 2 {
		t.Fatalf("got %d, want 2", got)
	}
	if got := CountUserMessages(nil); got != 0 {
		t.Fatalf("nil: got %d, want 0", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/sessionfile/... -run "IndexOfNthUser|CountUserMessages" -v`
Expected: FAIL to compile — `undefined: IndexOfNthUserMessage`, `undefined: CountUserMessages`.

- [ ] **Step 3: Write the implementation**

Create `internal/sessionfile/history.go`:

```go
package sessionfile

import "github.com/orchestra/orchestra/llm"

// IndexOfNthUserMessage returns the position of the nth (1-based) user message
// in hist, or -1 when hist holds fewer than n user messages.
//
// It returns a position rather than a slice because the two callers cut on
// opposite sides of it: rewind keeps the message it lands on, fork drops it.
func IndexOfNthUserMessage(hist []llm.Message, n int) int {
	if n <= 0 {
		return -1
	}
	seen := 0
	for i, m := range hist {
		if m.Role != llm.RoleUser {
			continue
		}
		seen++
		if seen == n {
			return i
		}
	}
	return -1
}

// CountUserMessages reports how many user messages a UI prefix holds. The UI
// projection and the LLM history are separate position-indexed arrays with no
// stable per-message id, so counting user turns is the only way to map one
// onto the other.
func CountUserMessages(ui []UIMessage) int {
	n := 0
	for _, m := range ui {
		if m.Role == "user" {
			n++
		}
	}
	return n
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/sessionfile/... -run "IndexOfNthUser|CountUserMessages" -v`
Expected: PASS.

- [ ] **Step 5: Rewrite rewind's helper over the locator**

In `internal/core/session_rewind.go`, replace the whole of `truncateHistoryForUIPrefix` (lines 79-107) with:

```go
// truncateHistoryForUIPrefix keeps LLM history through the last user message that
// corresponds to the user-message count in the UI prefix.
func truncateHistoryForUIPrefix(hist []llm.Message, ui []sessionfile.UIMessage) []llm.Message {
	userTarget := sessionfile.CountUserMessages(ui)
	if userTarget == 0 {
		return nil
	}
	if i := sessionfile.IndexOfNthUserMessage(hist, userTarget); i >= 0 {
		out := make([]llm.Message, i+1)
		copy(out, hist[:i+1])
		return out
	}
	// Compaction or partial sync — keep full history rather than truncate too far.
	out := make([]llm.Message, len(hist))
	copy(out, hist)
	return out
}
```

The `llm` and `sessionfile` imports are already present at `session_rewind.go:6-8`.

- [ ] **Step 6: Run rewind's existing tests as the regression proof**

Run: `go test ./internal/core/... -run "Rewind|TruncateHistory" -v`
Expected: PASS — `TestTruncateHistoryForUIPrefix_keepsThroughNthUser`, `TestTruncateHistoryForUIPrefix_firstUserOnly`, `TestSessionRewind_truncatesUIAndHistory`, `TestSessionRewind_rejectsNonUserIndex` all still pass unchanged. These are pre-existing tests; if any of them needs editing, the refactor changed behaviour and is wrong.

- [ ] **Step 7: Build, vet, full suite**

Run: `go build ./... && go vet ./...`, then `go test ./...`, then `go test -race ./internal/sessionfile/... ./internal/core/...`
Expected: all green.

- [ ] **Step 8: Commit**

```bash
git add internal/sessionfile/history.go internal/sessionfile/history_test.go internal/core/session_rewind.go
git commit -m "refactor(sessionfile): extract the user-message locator shared by rewind and fork"
```

---

## Task 2: ForkSnapshot and lineage fields

**Files:**
- Modify: `internal/sessionfile/snapshot.go:19-36` (two additive fields)
- Create: `internal/sessionfile/fork.go`
- Create: `internal/sessionfile/fork_test.go`

**Interfaces:**
- Consumes: `IndexOfNthUserMessage`, `CountUserMessages` (Task 1).
- Produces: `func ForkSnapshot(snap *Snapshot, uiIndex int, newID string) (*Snapshot, error)`; fields `Snapshot.ParentID string`, `Snapshot.ForkedFromIndex int`. Used by the `session.fork` RPC (Task 4) and the CLI (Task 5).

- [ ] **Step 1: Write the failing test**

Create `internal/sessionfile/fork_test.go`:

```go
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
		Todos:    []TodoItem{{Text: "left over"}},
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/sessionfile/... -run Fork -v`
Expected: FAIL to compile — `undefined: ForkSnapshot`, and `snap.ParentID` / `snap.ForkedFromIndex` are unknown fields.

- [ ] **Step 3: Add the lineage fields**

In `internal/sessionfile/snapshot.go`, inside the `Snapshot` struct, immediately after the `MsgCount` field (currently the last field, at line 35):

```go
	// ParentID and ForkedFromIndex record where a forked session branched from.
	// Additive with omitempty on purpose: LoadFromDisk rejects any snapshot
	// whose Version differs from the binary's own
	// (internal/core/session/persist.go:101-103), so bumping the schema would
	// make files written here unreadable by an older binary, while a field an
	// older binary does not know is simply ignored by json.Unmarshal.
	ParentID        string `json:"parent_id,omitempty"`
	ForkedFromIndex int    `json:"forked_from_index,omitempty"`
```

- [ ] **Step 4: Write the fork implementation**

Create `internal/sessionfile/fork.go`:

```go
package sessionfile

import (
	"errors"
	"fmt"

	"github.com/orchestra/orchestra/llm"
)

// ForkSnapshot returns a new snapshot holding everything strictly before
// uiIndex, leaving snap untouched. This is the non-destructive counterpart to
// session.rewind: the original session keeps every message it had.
//
// uiIndex must point at a user message — the same checkpoint rule rewind
// enforces — and the message it points at is NOT carried into the branch. The
// branch therefore ends with the assistant's answer to the previous turn, so
// the next thing written into it is a fresh prompt rather than a second user
// message in a row.
func ForkSnapshot(snap *Snapshot, uiIndex int, newID string) (*Snapshot, error) {
	if snap == nil {
		return nil, errors.New("fork: snapshot is nil")
	}
	if newID == "" {
		return nil, errors.New("fork: a new session id is required")
	}
	if uiIndex < 0 || uiIndex >= len(snap.UIMessages) {
		return nil, fmt.Errorf("fork: ui_message_index %d is out of range (session has %d messages)",
			uiIndex, len(snap.UIMessages))
	}
	if role := snap.UIMessages[uiIndex].Role; role != "user" {
		return nil, fmt.Errorf("fork: ui_message_index %d points at a %q message; a fork point must be a user message",
			uiIndex, role)
	}
	if uiIndex == 0 {
		return nil, errors.New("fork: cannot fork at the first message — the branch would be empty")
	}

	prefix := append([]UIMessage(nil), snap.UIMessages[:uiIndex]...)

	// uiIndex is a user message, so it is the (k+1)-th where k is the number of
	// user messages before it. The branch keeps history up to but not including
	// that turn.
	cut := IndexOfNthUserMessage(snap.History, CountUserMessages(prefix)+1)
	if cut < 0 {
		return nil, fmt.Errorf("fork: cannot map message %d onto the LLM history — this session was compacted, so the branch point no longer exists in history", uiIndex)
	}

	out := *snap
	out.ID = newID
	out.UIMessages = prefix
	out.History = append([]llm.Message(nil), snap.History[:cut]...)
	out.MsgCount = len(prefix)
	out.ParentID = snap.ID
	out.ForkedFromIndex = uiIndex
	out.Title = forkTitle(snap.Title)

	// Everything below describes the path the branch is abandoning: pending ops
	// and todos are what rewind clears too, spend belongs to the parent's
	// session (counting it twice would inflate the project total), and apply
	// output refers to work the branch does not contain.
	out.PendingOps = nil
	out.Todos = nil
	out.CostUSD = 0
	out.ApplyOutput = ""

	return &out, nil
}

// forkTitle marks a branch so the session picker does not show two identical
// rows: titles are derived from the first user message, which a branch shares
// with its parent verbatim.
func forkTitle(parent string) string {
	if parent == "" {
		return "(fork)"
	}
	return parent + " (fork)"
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/sessionfile/... -v`
Expected: PASS — the new fork tests plus every pre-existing `sessionfile` test.

- [ ] **Step 6: Build, vet, full suite**

Run: `go build ./... && go vet ./...`, then `go test ./...`, then `go test -race ./internal/sessionfile/...`
Expected: all green.

- [ ] **Step 7: Commit**

```bash
git add internal/sessionfile/fork.go internal/sessionfile/fork_test.go internal/sessionfile/snapshot.go
git commit -m "feat(sessionfile): add ForkSnapshot and additive fork lineage fields"
```

---

## Task 3: Content search

**Files:**
- Create: `internal/sessionfile/search.go`
- Create: `internal/sessionfile/search_test.go`

**Interfaces:**
- Produces: `type Hit struct{...}`, `type SearchOptions struct{...}`, `func Search(workspaceRoot string, opts SearchOptions) ([]Hit, error)`. Used by the CLI (Task 5), the TUI (Task 6) and the `session.search` RPC (Task 4).

- [ ] **Step 1: Write the failing test**

Create `internal/sessionfile/search_test.go`:

```go
package sessionfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// saveSearchFixture writes one session file directly rather than through Save,
// because Save stamps UpdatedAt with time.Now() and these tests assert on
// ordering by update time.
func saveSearchFixture(t *testing.T, root, id, title string, updated time.Time, msgs []UIMessage) {
	t.Helper()
	snap := &Snapshot{
		Version:    Version,
		ID:         id,
		Title:      title,
		UIMessages: msgs,
		CreatedAt:  updated,
		UpdatedAt:  updated,
		MsgCount:   len(msgs),
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".orchestra", "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSearch_FindsAMessageAndReportsItsIndex(t *testing.T) {
	root := t.TempDir()
	saveSearchFixture(t, root, "20260901T100000-aaaa", "first", time.Now().Add(-time.Hour), []UIMessage{
		{Role: "user", Text: "how do I wire the bearer token"},
		{Role: "assistant", Text: "authTransport sets the header"},
	})

	hits, err := Search(root, SearchOptions{Query: "bearer"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1: %+v", len(hits), hits)
	}
	h := hits[0]
	if h.SessionID != "20260901T100000-aaaa" {
		t.Errorf("SessionID = %q", h.SessionID)
	}
	// The index is what `session fork --at` and session.rewind both take.
	if h.Index != 0 {
		t.Errorf("Index = %d, want 0", h.Index)
	}
	if h.Role != "user" {
		t.Errorf("Role = %q, want user", h.Role)
	}
	if !strings.Contains(h.Snippet, "bearer") {
		t.Errorf("Snippet = %q, want it to carry the match", h.Snippet)
	}
}

func TestSearch_IsCaseSensitiveByDefaultAndInsensitiveOnRequest(t *testing.T) {
	root := t.TempDir()
	saveSearchFixture(t, root, "20260901T100000-aaaa", "t", time.Now(), []UIMessage{
		{Role: "user", Text: "Bearer token"},
	})

	// Mirrors `orchestra search`, which defaults to case-sensitive (internal/cli/search.go:27).
	if hits, err := Search(root, SearchOptions{Query: "bearer"}); err != nil || len(hits) != 0 {
		t.Fatalf("case-sensitive search matched anyway: %v %+v", err, hits)
	}
	hits, err := Search(root, SearchOptions{Query: "bearer", Insensitive: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("insensitive hits = %d, want 1", len(hits))
	}
}

func TestSearch_SkipsToolBlocksUnlessIncludeAll(t *testing.T) {
	root := t.TempDir()
	saveSearchFixture(t, root, "20260901T100000-aaaa", "t", time.Now(), []UIMessage{
		{
			Role: "assistant",
			Text: "done",
			ToolBlocks: []UIToolBlock{
				{Name: "read", Result: "package remote contains authTransport"},
			},
		},
	})

	// Tool output is large and noisy; by default it must not bury prose hits.
	if hits, err := Search(root, SearchOptions{Query: "authTransport"}); err != nil || len(hits) != 0 {
		t.Fatalf("default search reached tool output: %v %+v", err, hits)
	}
	hits, err := Search(root, SearchOptions{Query: "authTransport", IncludeAll: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("--all hits = %d, want 1", len(hits))
	}
}

func TestSearch_OneHitPerMessage(t *testing.T) {
	root := t.TempDir()
	saveSearchFixture(t, root, "20260901T100000-aaaa", "t", time.Now(), []UIMessage{
		{Role: "user", Text: "token token token token"},
	})

	hits, err := Search(root, SearchOptions{Query: "token"})
	if err != nil {
		t.Fatal(err)
	}
	// Four occurrences in one message must not become four rows.
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
}

func TestSearch_OrdersRecentSessionsFirstAndCapsWithLimit(t *testing.T) {
	root := t.TempDir()
	older := time.Now().Add(-48 * time.Hour)
	newer := time.Now().Add(-1 * time.Hour)
	saveSearchFixture(t, root, "20260901T100000-aaaa", "old", older, []UIMessage{
		{Role: "user", Text: "token a"},
		{Role: "user", Text: "token b"},
	})
	saveSearchFixture(t, root, "20260903T100000-bbbb", "new", newer, []UIMessage{
		{Role: "user", Text: "token c"},
	})

	hits, err := Search(root, SearchOptions{Query: "token"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 3 {
		t.Fatalf("hits = %d, want 3", len(hits))
	}
	if hits[0].SessionID != "20260903T100000-bbbb" {
		t.Errorf("most recently updated session must come first, got %q", hits[0].SessionID)
	}
	// Within a session, message order is ascending.
	if hits[1].Index != 0 || hits[2].Index != 1 {
		t.Errorf("in-session order = %d,%d, want 0,1", hits[1].Index, hits[2].Index)
	}

	capped, err := Search(root, SearchOptions{Query: "token", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(capped) != 2 {
		t.Fatalf("limited hits = %d, want 2", len(capped))
	}
	if capped[0].SessionID != "20260903T100000-bbbb" {
		t.Errorf("the cap must keep the most recent sessions, got %q", capped[0].SessionID)
	}
}

func TestSearch_SkipsUnreadableFilesInsteadOfFailing(t *testing.T) {
	root := t.TempDir()
	saveSearchFixture(t, root, "20260901T100000-aaaa", "good", time.Now(), []UIMessage{
		{Role: "user", Text: "token here"},
	})
	// One corrupt file must not take the other fifty-one down with it.
	if err := os.WriteFile(filepath.Join(root, ".orchestra", "sessions", "broken.json"),
		[]byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	hits, err := Search(root, SearchOptions{Query: "token"})
	if err != nil {
		t.Fatalf("a corrupt file must not fail the search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
}

func TestSearch_EmptyQueryAndMissingDirectory(t *testing.T) {
	root := t.TempDir()
	if _, err := Search(root, SearchOptions{Query: "  "}); err == nil {
		t.Error("an empty query must be refused")
	}
	// A project that has never had a session is not an error.
	hits, err := Search(root, SearchOptions{Query: "token"})
	if err != nil {
		t.Fatalf("missing sessions dir must not be an error: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("hits = %+v, want none", hits)
	}
}

func TestSnippetAround_TrimsAndMarksElision(t *testing.T) {
	long := strings.Repeat("a ", 200) + "needle " + strings.Repeat("b ", 200)
	got := snippetAround(long, "needle", false)
	if !strings.Contains(got, "needle") {
		t.Fatalf("snippet lost the match: %q", got)
	}
	if len([]rune(got)) > snippetWidth+2 {
		t.Fatalf("snippet is %d runes, want at most %d plus the two ellipses", len([]rune(got)), snippetWidth)
	}
	if !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
		t.Fatalf("both ends were cut, so both should be marked: %q", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/sessionfile/... -run "Search|Snippet" -v`
Expected: FAIL to compile — `undefined: Search`, `undefined: SearchOptions`, `undefined: snippetAround`, `undefined: snippetWidth`.

- [ ] **Step 3: Write the implementation**

Create `internal/sessionfile/search.go`:

```go
package sessionfile

import (
	"errors"
	"os"
	"sort"
	"strings"
	"time"
)

// snippetWidth caps how much of a matching message is shown, in runes.
const snippetWidth = 120

// Hit is one matching message inside one session.
type Hit struct {
	SessionID string    `json:"session_id"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updated_at"`
	// Index is the position in ui_messages — the same index session.fork and
	// session.rewind take, so a search result can be acted on directly.
	Index   int    `json:"index"`
	Role    string `json:"role"`
	Snippet string `json:"snippet"`
}

// SearchOptions configures a session content search.
type SearchOptions struct {
	Query string
	// Insensitive mirrors `orchestra search -i`; the default is case-sensitive
	// so two sibling commands do not disagree.
	Insensitive bool
	// IncludeAll also searches reasoning and tool blocks, not just message text.
	IncludeAll bool
	// Limit caps the number of hits returned; 0 means no cap.
	Limit int
}

// Search scans every session in the project for messages containing the query.
//
// It parses each session file, exactly as ListMeta already does for the picker,
// so this adds work on bytes that were being read anyway rather than a new
// class of I/O. Files that cannot be read or parsed are skipped: one corrupt
// session must not make search fail for all the others.
func Search(workspaceRoot string, opts SearchOptions) ([]Hit, error) {
	if strings.TrimSpace(opts.Query) == "" {
		return nil, errors.New("search: query is empty")
	}
	entries, err := os.ReadDir(sessionsDir(workspaceRoot))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	needle := opts.Query
	if opts.Insensitive {
		needle = strings.ToLower(needle)
	}

	type group struct {
		updatedAt time.Time
		hits      []Hit
	}
	var groups []group

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		snap, err := Load(workspaceRoot, strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			continue
		}
		var hits []Hit
		for i, m := range snap.UIMessages {
			field, ok := matchField(m, needle, opts)
			if !ok {
				continue
			}
			hits = append(hits, Hit{
				SessionID: snap.ID,
				Title:     snap.Title,
				UpdatedAt: snap.UpdatedAt,
				Index:     i,
				Role:      m.Role,
				Snippet:   snippetAround(field, needle, opts.Insensitive),
			})
		}
		if len(hits) > 0 {
			groups = append(groups, group{updatedAt: snap.UpdatedAt, hits: hits})
		}
	}

	sort.SliceStable(groups, func(i, j int) bool {
		return groups[i].updatedAt.After(groups[j].updatedAt)
	})

	var out []Hit
	for _, g := range groups {
		for _, h := range g.hits {
			if opts.Limit > 0 && len(out) >= opts.Limit {
				return out, nil
			}
			out = append(out, h)
		}
	}
	return out, nil
}

// matchField returns the first field of m that contains needle, so the caller
// can build a snippet from it. Only one field is reported per message: a tool
// result mentioning the query forty times must not become forty rows.
func matchField(m UIMessage, needle string, opts SearchOptions) (string, bool) {
	fields := []string{m.Text}
	if opts.IncludeAll {
		fields = append(fields, m.Reasoning)
		for _, tb := range m.ToolBlocks {
			fields = append(fields, tb.Name, tb.ArgsPreview, tb.ArgsRaw, tb.Result)
		}
		for _, seg := range m.Segments {
			fields = append(fields, seg.Text)
			for _, tb := range seg.Tools {
				fields = append(fields, tb.Name, tb.ArgsPreview, tb.ArgsRaw, tb.Result)
			}
		}
	}
	for _, f := range fields {
		if f == "" {
			continue
		}
		hay := f
		if opts.Insensitive {
			hay = strings.ToLower(f)
		}
		if strings.Contains(hay, needle) {
			return f, true
		}
	}
	return "", false
}

// snippetAround renders a single-line window of text centred on the first
// occurrence of needle, capped at snippetWidth runes with … marking each cut end.
func snippetAround(text, needle string, insensitive bool) string {
	flat := strings.Join(strings.Fields(text), " ")
	runes := []rune(flat)
	if len(runes) <= snippetWidth {
		return flat
	}

	hay := flat
	if insensitive {
		hay = strings.ToLower(flat)
	}
	// Case folding can change byte length for a few runes, so the offset is
	// clamped before it is used to slice.
	byteAt := strings.Index(hay, needle)
	if byteAt < 0 || byteAt > len(flat) {
		byteAt = 0
	}
	matchAt := len([]rune(flat[:byteAt]))

	start := matchAt - snippetWidth/3
	if start < 0 {
		start = 0
	}
	end := start + snippetWidth
	if end > len(runes) {
		end = len(runes)
		start = end - snippetWidth
	}

	out := string(runes[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(runes) {
		out += "…"
	}
	return out
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/sessionfile/... -v`
Expected: PASS.

- [ ] **Step 5: Build, vet, full suite**

Run: `go build ./... && go vet ./...`, then `go test ./...`, then `go test -race ./internal/sessionfile/...`
Expected: all green.

- [ ] **Step 6: Commit**

```bash
git add internal/sessionfile/search.go internal/sessionfile/search_test.go
git commit -m "feat(sessionfile): add message-level content search across sessions"
```

---

## Task 4: RPC methods `session.fork` and `session.search`

`session.fork` must go through core because the Manager holds the authoritative
in-memory copy of a live session and the file on disk lags it by up to the
5-second mid-turn snapshot interval.

**Files:**
- Create: `internal/core/session_fork.go`
- Create: `internal/core/session_fork_test.go`
- Create: `internal/core/session_search.go`
- Modify: `internal/core/rpc_handler.go` (two `case` arms, next to `session.rewind` at :185-192)
- Modify: `ui/tui/rpcclient/client.go` (stub + mirrored result type, following `SessionRewind` at :244-262)
- Modify: `ui/tui/coreclient.go:28` (interface) and `ui/tui/coreclient_fake_test.go` (fake)
- Modify: `protocol/version.go:23` (13 → 14) and `docs/PROTOCOL.md`

**Interfaces:**
- Consumes: `sessionfile.ForkSnapshot`, `sessionfile.Search`, `sessionfile.NewID` (Tasks 2-3).
- Produces: `(*Core).SessionFork(SessionForkParams) (*SessionForkResult, error)`, `(*Core).SessionSearch(SessionSearchParams) (*SessionSearchResult, error)`, and `(*rpcclient.Client).SessionFork(ctx, sessionID string, uiMessageIndex int) (*rpcclient.SessionForkResult, error)` used by the TUI in Task 7.

- [ ] **Step 1: Write the failing test**

Create `internal/core/session_fork_test.go`:

```go
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
	sess.ReplaceHistory([]llm.Message{
		{Role: llm.RoleUser, Content: "u1"},
		{Role: llm.RoleAssistant, Content: "a1"},
		{Role: llm.RoleUser, Content: "u2"},
		{Role: llm.RoleAssistant, Content: "a2"},
	})
	if err := sess.Snapshot(c.workspaceRoot); err != nil {
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
	// next session.start must pick it up from disk.
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/core/... -run "SessionFork|SessionSearch" -v`
Expected: FAIL to compile — `undefined: SessionForkParams`, `undefined: SessionSearchParams`, and the `Core` methods do not exist.

- [ ] **Step 3: Write `session.fork`**

Create `internal/core/session_fork.go`:

```go
package core

import (
	"strings"

	"github.com/orchestra/orchestra/internal/sessionfile"
	"github.com/orchestra/orchestra/protocol"
)

type SessionForkParams struct {
	SessionID      string `json:"session_id"`
	UIMessageIndex int    `json:"ui_message_index"` // exclusive; must point at role=user
}

type SessionForkResult struct {
	SessionID       string `json:"session_id"` // the new branch
	ParentID        string `json:"parent_id"`
	UIMessages      int    `json:"ui_messages"`
	HistoryMessages int    `json:"history_messages"`
}

// SessionFork copies a session's history up to (but not including) a user
// checkpoint into a new session, leaving the original untouched. This is the
// non-destructive counterpart to SessionRewind.
//
// It reads the parent from memory rather than from disk: the Manager holds the
// authoritative copy and the snapshot on disk lags a live session by up to the
// mid-turn snapshot interval.
func (c *Core) SessionFork(params SessionForkParams) (*SessionForkResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	sid := strings.TrimSpace(params.SessionID)
	if sid == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "session_id is empty", nil)
	}
	sess, err := c.sessions.GetOrLoad(c.workspaceRoot, sid)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), map[string]any{"session_id": sid})
	}

	sess.Lock()
	defer sess.Unlock()

	if sess.IsBusy() {
		return nil, protocol.NewError(protocol.ExecFailed, "session is busy", map[string]any{"session_id": sid})
	}

	// Built from live accessors rather than sessionfile.Load: todos, pending
	// ops, spend and apply output are all cleared by ForkSnapshot anyway, so
	// only the fields the branch actually inherits are carried over.
	src := &sessionfile.Snapshot{
		Version:    sessionfile.Version,
		ID:         sid,
		Title:      sess.Title(),
		Model:      sess.Model(),
		Profile:    sess.Profile(),
		PlanPath:   sess.PlanPath(),
		UIMessages: sess.UIMessages(),
		History:    sess.CopyHistory(),
	}

	branch, err := sessionfile.ForkSnapshot(src, params.UIMessageIndex, sessionfile.NewID())
	if err != nil {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, err.Error(), map[string]any{
			"session_id": sid,
			"index":      params.UIMessageIndex,
		})
	}
	if err := sessionfile.Save(c.workspaceRoot, branch); err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), map[string]any{
			"session_id": branch.ID,
		})
	}

	// Deliberately not registered with the Manager: the client's following
	// session.start loads it from disk through LoadOrCreate, so the branch
	// never has two owners.
	return &SessionForkResult{
		SessionID:       branch.ID,
		ParentID:        sid,
		UIMessages:      len(branch.UIMessages),
		HistoryMessages: len(branch.History),
	}, nil
}
```

- [ ] **Step 4: Write `session.search`**

Create `internal/core/session_search.go`:

```go
package core

import (
	"github.com/orchestra/orchestra/internal/sessionfile"
	"github.com/orchestra/orchestra/protocol"
)

type SessionSearchParams struct {
	Query       string `json:"query"`
	Insensitive bool   `json:"insensitive,omitempty"`
	IncludeAll  bool   `json:"include_all,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

type SessionSearchResult struct {
	Hits []sessionfile.Hit `json:"hits"`
}

// SessionSearch finds messages containing the query across every saved session
// in the workspace.
func (c *Core) SessionSearch(params SessionSearchParams) (*SessionSearchResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	hits, err := sessionfile.Search(c.workspaceRoot, sessionfile.SearchOptions{
		Query:       params.Query,
		Insensitive: params.Insensitive,
		IncludeAll:  params.IncludeAll,
		Limit:       params.Limit,
	})
	if err != nil {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, err.Error(), nil)
	}
	return &SessionSearchResult{Hits: hits}, nil
}
```

- [ ] **Step 5: Register both methods**

In `internal/core/rpc_handler.go`, immediately after the `case "session.rewind":` arm (which ends at line 192), add:

```go
	case "session.fork":
		var p SessionForkParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.SessionFork(p)

	case "session.search":
		var p SessionSearchParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.SessionSearch(p)
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/core/... -run "SessionFork|SessionSearch" -v`
Expected: PASS.

- [ ] **Step 7: Add the TUI client stub**

In `ui/tui/rpcclient/client.go`, directly after the `SessionRewind` method (which ends at line 262), add:

```go
// SessionForkResult mirrors core.SessionForkResult.
type SessionForkResult struct {
	SessionID       string `json:"session_id"`
	ParentID        string `json:"parent_id"`
	UIMessages      int    `json:"ui_messages"`
	HistoryMessages int    `json:"history_messages"`
}

// SessionFork branches a session at a user checkpoint, leaving the original intact.
func (c *Client) SessionFork(ctx context.Context, sessionID string, uiMessageIndex int) (*SessionForkResult, error) {
	var res SessionForkResult
	err := c.rpc.Call(ctx, "session.fork", map[string]any{
		"session_id":       sessionID,
		"ui_message_index": uiMessageIndex,
	}, &res)
	if err != nil {
		return nil, err
	}
	return &res, nil
}
```

There is deliberately no `SessionSearch` client stub: the TUI reads session files
directly through `sessionfile`, exactly as it already does for `ListMeta`, so
routing search through the core process would add a round trip for no gain. The
RPC method exists for out-of-process clients such as the VS Code extension.

- [ ] **Step 8: Extend the CoreClient interface and its fake**

In `ui/tui/coreclient.go`, add to the "Session lifecycle / persistence" block, right after the `SessionRewind` line:

```go
	SessionFork(ctx context.Context, sessionID string, uiMessageIndex int) (*rpcclient.SessionForkResult, error)
```

In `ui/tui/coreclient_fake_test.go`, next to the existing `SessionRewind` fake method, add:

```go
func (f *fakeCore) SessionFork(ctx context.Context, sessionID string, uiMessageIndex int) (*rpcclient.SessionForkResult, error) {
	f.forkedSessionID = sessionID
	f.forkedIndex = uiMessageIndex
	if f.forkErr != nil {
		return nil, f.forkErr
	}
	return &rpcclient.SessionForkResult{
		SessionID:  "forked-session-id",
		ParentID:   sessionID,
		UIMessages: uiMessageIndex,
	}, nil
}
```

and add the three fields `forkedSessionID string`, `forkedIndex int`, `forkErr error` to the `fakeCore` struct. Read the file first to match its existing field-block layout and the exact receiver name it uses — missing either the interface entry or the fake breaks the build.

- [ ] **Step 9: Document both methods**

The protocol version is currently **13**: `protocol/version.go:23` has
`ProtocolVersion = 13` and `docs/PROTOCOL.md:7` states the same. Both must move
to 14 together — `initialize` requires an exact match, so a doc-only or
code-only bump breaks every client handshake.

In `protocol/version.go:23`:

```go
	ProtocolVersion = 14
```

In `docs/PROTOCOL.md:7`, update the stated version:

```markdown
- **`protocol.ProtocolVersion`**: `14`
```

In the `### История ProtocolVersion` list, add a line above the existing `- **v13**` entry:

```markdown
- **v14** (2026-09-06): `session.fork` — ветка от user-чекпоинта без разрушения оригинала; `session.search` — поиск по тексту сообщений во всех сохранённых сессиях.
```

Then add two method sections next to the existing `session.rewind` section (`docs/PROTOCOL.md:535-548`), each documenting params and result with a JSON example, following that section's formatting.

- [ ] **Step 10: Build, vet, full suite**

Run: `go build ./... && go vet ./...`, then `go test ./...`, then `go test -race ./internal/core/... ./ui/tui/...`
Expected: all green.

- [ ] **Step 11: Commit**

```bash
git add internal/core/session_fork.go internal/core/session_fork_test.go internal/core/session_search.go internal/core/rpc_handler.go ui/tui/rpcclient/client.go ui/tui/coreclient.go ui/tui/coreclient_fake_test.go protocol/version.go docs/PROTOCOL.md
git commit -m "feat(core): add session.fork and session.search RPC methods"
```

---

## Task 5: CLI `session search` and `session fork`

**Files:**
- Modify: `internal/cli/session.go` (two subcommands, `init()` wiring at :49-57)
- Modify: `internal/cli/session_test.go`

**Interfaces:**
- Consumes: `sessionfile.Search`, `sessionfile.SearchOptions`, `sessionfile.Hit`, `sessionfile.ForkSnapshot`, `sessionfile.Load`, `sessionfile.Save`, `sessionfile.NewID`.

- [ ] **Step 1: Write the failing test**

Add to `internal/cli/session_test.go` (it already has the fixture technique this uses — read the existing `TestSessionExportImportCLI` first and reuse its temp-project setup):

```go
func TestSessionSearchAndForkCLI(t *testing.T) {
	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	if err := os.WriteFile(filepath.Join(dir, ".orchestra.yml"),
		[]byte("project_root: .\nllm:\n  api_base: http://localhost:1234/v1\n  model: m\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	snap := &sessionfile.Snapshot{
		Version: sessionfile.Version,
		ID:      "20260901T100000-aaaa",
		Title:   "wire the bearer token",
		UIMessages: []sessionfile.UIMessage{
			{Role: "user", Text: "wire the bearer token"},
			{Role: "assistant", Text: "authTransport sets the header"},
			{Role: "user", Text: "now do it differently"},
			{Role: "assistant", Text: "ok"},
		},
		History: []llm.Message{
			{Role: llm.RoleUser, Content: "wire the bearer token"},
			{Role: llm.RoleAssistant, Content: "authTransport sets the header"},
			{Role: llm.RoleUser, Content: "now do it differently"},
			{Role: llm.RoleAssistant, Content: "ok"},
		},
	}
	if err := sessionfile.Save(dir, snap); err != nil {
		t.Fatal(err)
	}

	sessionSearchInsensitive = false
	sessionSearchAll = false
	sessionSearchLimit = 0
	if err := runSessionSearch(nil, []string{"bearer"}); err != nil {
		t.Fatalf("session search: %v", err)
	}

	// A query that matches nothing is not a failure.
	if err := runSessionSearch(nil, []string{"zzz-no-such-text"}); err != nil {
		t.Fatalf("a query with no matches must exit cleanly: %v", err)
	}

	sessionForkAt = 2
	if err := runSessionFork(nil, []string{"20260901T100000-aaaa"}); err != nil {
		t.Fatalf("session fork: %v", err)
	}

	metas, err := sessionfile.ListMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 2 {
		t.Fatalf("want the original plus one branch, got %d", len(metas))
	}
	var branchID string
	for _, m := range metas {
		if m.ID != "20260901T100000-aaaa" {
			branchID = m.ID
		}
	}
	branch, err := sessionfile.Load(dir, branchID)
	if err != nil {
		t.Fatal(err)
	}
	if len(branch.UIMessages) != 2 {
		t.Fatalf("branch UIMessages = %d, want 2", len(branch.UIMessages))
	}
	if branch.ParentID != "20260901T100000-aaaa" {
		t.Errorf("ParentID = %q", branch.ParentID)
	}

	// Forking a non-user index must fail rather than produce a broken branch.
	sessionForkAt = 1
	if err := runSessionFork(nil, []string{"20260901T100000-aaaa"}); err == nil {
		t.Error("forking at an assistant message must be refused")
	}
}
```

Add `"github.com/orchestra/orchestra/llm"` to the test file's imports if it is not already there.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cli/... -run SessionSearchAndFork -v`
Expected: FAIL to compile — `undefined: runSessionSearch`, `undefined: runSessionFork`, and the flag variables do not exist.

- [ ] **Step 3: Write the subcommands**

In `internal/cli/session.go`, add to the flag `var` block at lines 22-26:

```go
	sessionSearchInsensitive bool
	sessionSearchAll         bool
	sessionSearchLimit       int
	sessionForkAt            int
```

Add the two command definitions after `sessionImportCmd` (line 47):

```go
var sessionSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search message text across saved sessions",
	Long: `Searches the text of every message in every session of this project and
prints one line per matching message, with the message index that
'orchestra session fork --at' and the TUI checkpoint picker both use.

By default only user and assistant message text is searched; --all adds
reasoning and tool blocks, which are much larger and noisier.`,
	Args: cobra.ExactArgs(1),
	RunE: runSessionSearch,
}

var sessionForkCmd = &cobra.Command{
	Use:   "fork <session-id>",
	Short: "Branch a session at a checkpoint, keeping the original intact",
	Long: `Creates a new session containing this session's history up to, but not
including, the message at --at, which must be a user message. The original
session is left exactly as it was — unlike rewind, nothing is destroyed.

Use 'orchestra session search' to find the index to branch at. This reads the
session file on disk, so a session currently open elsewhere may be missing its
most recent few seconds.`,
	Args: cobra.ExactArgs(1),
	RunE: runSessionFork,
}
```

Extend `init()` (lines 49-57):

```go
	sessionSearchCmd.Flags().BoolVarP(&sessionSearchInsensitive, "insensitive", "i", false, "Case-insensitive search")
	sessionSearchCmd.Flags().BoolVar(&sessionSearchAll, "all", false, "Also search reasoning and tool blocks")
	sessionSearchCmd.Flags().IntVar(&sessionSearchLimit, "limit", 0, "Maximum number of matches (0 = no limit)")
	sessionForkCmd.Flags().IntVar(&sessionForkAt, "at", -1, "Index of the user message to branch at (see 'session search')")
	sessionCmd.AddCommand(sessionSearchCmd)
	sessionCmd.AddCommand(sessionForkCmd)
```

Add the two handlers at the end of the file:

```go
func runSessionSearch(cmd *cobra.Command, args []string) error {
	cfg, err := loadProjectConfig()
	if err != nil {
		return err
	}
	hits, err := sessionfile.Search(cfg.ProjectRoot, sessionfile.SearchOptions{
		Query:       args[0],
		Insensitive: sessionSearchInsensitive,
		IncludeAll:  sessionSearchAll,
		Limit:       sessionSearchLimit,
	})
	if err != nil {
		return err
	}
	if len(hits) == 0 {
		fmt.Println("No matches.")
		return nil
	}

	currentSession := ""
	for _, h := range hits {
		if h.SessionID != currentSession {
			currentSession = h.SessionID
			title := strings.ReplaceAll(h.Title, "\n", " ")
			fmt.Printf("\n%s  %s  (%s)\n", h.SessionID, title, h.UpdatedAt.UTC().Format(time.RFC3339))
		}
		fmt.Printf("  #%-4d %-9s %s\n", h.Index, h.Role, h.Snippet)
	}
	return nil
}

func runSessionFork(cmd *cobra.Command, args []string) error {
	cfg, err := loadProjectConfig()
	if err != nil {
		return err
	}
	if sessionForkAt < 0 {
		return fmt.Errorf("--at is required: pass the index of the user message to branch at (see 'orchestra session search')")
	}
	id := strings.TrimSpace(args[0])
	snap, err := sessionfile.Load(cfg.ProjectRoot, id)
	if err != nil {
		return err
	}
	branch, err := sessionfile.ForkSnapshot(snap, sessionForkAt, sessionfile.NewID())
	if err != nil {
		return err
	}
	if err := sessionfile.Save(cfg.ProjectRoot, branch); err != nil {
		return err
	}
	fmt.Println(branch.ID)
	fmt.Fprintf(os.Stderr, "Forked %q at message %d → %s (%d messages)\n",
		id, sessionForkAt, branch.ID, len(branch.UIMessages))
	return nil
}
```

The `fmt`, `os`, `strings`, `time` and `sessionfile` imports are already present
at `internal/cli/session.go:3-14`.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/cli/... -run SessionSearchAndFork -v`
Expected: PASS.

- [ ] **Step 5: Check the commands by hand**

Run from any project that has sessions:
```bash
go run ./cmd/orchestra session search token
go run ./cmd/orchestra session search token -i --limit 5
```
Expected: grouped output with `#<index>` per hit, or `No matches.` — and a non-zero index that can be passed to `session fork --at`.

- [ ] **Step 6: Build, vet, full suite**

Run: `go build ./... && go vet ./...`, then `go test ./...`, then `go test -race ./internal/cli/...`
Expected: all green.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/session.go internal/cli/session_test.go
git commit -m "feat(cli): add orchestra session search and session fork"
```

---

## Task 6: TUI content filter — `/sessions <query>`

Bare `/sessions` keeps today's behaviour byte for byte; only the form with a
query is new, so it is added as a prefix check before the existing switch — the
same shape `parseMCPPromptCommand` already uses for the other argument-carrying
slash command (`ui/tui/app_palette.go:286-289`).

**Files:**
- Modify: `ui/tui/app_session.go` (new `openSessionsDialogFiltered`)
- Modify: `ui/tui/app_palette.go` (prefix check before `switch cmd` at :291)
- Create: `ui/tui/app_session_search_test.go`

**Interfaces:**
- Consumes: `sessionfile.Search` (Task 3).

- [ ] **Step 1: Write the failing test**

Create `ui/tui/app_session_search_test.go`:

```go
package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/sessionfile"
)

func TestSessionsMatchingQuery_FiltersByMessageText(t *testing.T) {
	a := testChromeApp(t)
	// testChromeApp builds an App with Config{Model, Mode, CWD} only
	// (ui/tui/app_chrome_test.go:11-22), so WorkspaceRoot is empty and every
	// session read/write would land outside a temp dir. Point it at one.
	root := t.TempDir()
	a.cfg.WorkspaceRoot = root

	save := func(id, text string) {
		t.Helper()
		snap := &sessionfile.Snapshot{
			Version:    sessionfile.Version,
			ID:         id,
			Title:      id,
			UIMessages: []sessionfile.UIMessage{{Role: "user", Text: text}},
		}
		if err := sessionfile.Save(root, snap); err != nil {
			t.Fatal(err)
		}
	}
	save("20260901T100000-aaaa", "wire the bearer token")
	save("20260902T100000-bbbb", "something else entirely")

	metas, err := a.sessionsMatchingQuery("bearer")
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 1 {
		t.Fatalf("metas = %d, want 1: %+v", len(metas), metas)
	}
	if metas[0].ID != "20260901T100000-aaaa" {
		t.Fatalf("ID = %q", metas[0].ID)
	}
}

func TestSessionsMatchingQuery_NoMatchesIsNotAnError(t *testing.T) {
	a := testChromeApp(t)
	a.cfg.WorkspaceRoot = t.TempDir()
	if err := os.MkdirAll(filepath.Join(a.cfg.WorkspaceRoot, ".orchestra", "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	metas, err := a.sessionsMatchingQuery("nothing-matches-this")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(metas) != 0 {
		t.Fatalf("metas = %+v, want none", metas)
	}
}

func TestParseSessionsQuery(t *testing.T) {
	for _, tc := range []struct {
		in    string
		query string
		ok    bool
	}{
		{"/sessions bearer token", "bearer token", true},
		{"/sessions   spaced  ", "spaced", true},
		{"/sessions", "", false}, // the bare form keeps its existing path
		{"/sessionsfoo", "", false},
		{"/rewind", "", false},
	} {
		query, ok := parseSessionsQuery(tc.in)
		if ok != tc.ok || query != tc.query {
			t.Errorf("parseSessionsQuery(%q) = (%q,%v), want (%q,%v)", tc.in, query, ok, tc.query, tc.ok)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./ui/tui/... -run "SessionsMatchingQuery|ParseSessionsQuery" -v`
Expected: FAIL to compile — `undefined: sessionsMatchingQuery`, `undefined: parseSessionsQuery`.

- [ ] **Step 3: Write the implementation**

In `ui/tui/app_session.go`, add after `openSessionsDialog` (which ends at line 241):

```go
// sessionsMatchingQuery returns the metadata of sessions whose message text
// contains query, preserving ListMeta's most-recently-updated-first order.
func (a *App) sessionsMatchingQuery(query string) ([]sessionstore.SessionMeta, error) {
	hits, err := sessionfile.Search(a.cfg.WorkspaceRoot, sessionfile.SearchOptions{
		Query:       query,
		Insensitive: true, // typing a filter in a picker is not a case-sensitive act
	})
	if err != nil {
		return nil, err
	}
	matched := make(map[string]bool, len(hits))
	for _, h := range hits {
		matched[h.SessionID] = true
	}
	all, err := sessionstore.List(a.cfg.WorkspaceRoot)
	if err != nil {
		return nil, err
	}
	out := make([]sessionstore.SessionMeta, 0, len(matched))
	for _, m := range all {
		if matched[m.ID] {
			out = append(out, m)
		}
	}
	return out, nil
}

// openSessionsDialogFiltered opens the session picker seeded with only the
// sessions whose text matches query. The dialog itself is unchanged — it
// already accepts a prepared list, and its own fuzzy filter over titles keeps
// working on top of the narrowed set.
func (a *App) openSessionsDialogFiltered(query string) {
	metas, err := a.sessionsMatchingQuery(query)
	if err != nil {
		a.showToast("поиск по сессиям: " + err.Error())
		return
	}
	if len(metas) == 0 {
		a.showToast("ничего не найдено: " + query)
		return
	}
	a.dialogStack = append(a.dialogStack, view.NewSessionsDialog(metas))
}
```

Add `"github.com/orchestra/orchestra/internal/sessionfile"` to `app_session.go`'s imports if it is not already there.

In `ui/tui/app_palette.go`, add the parser next to `parseMCPPromptCommand`'s call site — insert immediately before `switch cmd {` at line 291:

```go
	// `/sessions <query>` filters the picker by message text. The bare form
	// keeps its existing case below, so today's behaviour is untouched.
	if query, ok := parseSessionsQuery(cmd); ok {
		a.openSessionsDialogFiltered(query)
		return nil
	}
```

and add the helper at the end of `app_palette.go`:

```go
// parseSessionsQuery splits "/sessions <query>" into its query. The bare
// "/sessions" returns ok=false so it falls through to the plain picker.
func parseSessionsQuery(cmd string) (string, bool) {
	rest, ok := strings.CutPrefix(cmd, "/sessions ")
	if !ok {
		return "", false
	}
	query := strings.TrimSpace(rest)
	if query == "" {
		return "", false
	}
	return query, true
}
```

Verify `strings` is imported in `app_palette.go`; add it if not.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./ui/tui/... -run "SessionsMatchingQuery|ParseSessionsQuery" -v`
Expected: PASS.

- [ ] **Step 5: Register the argument form in the palette help**

In `ui/tui/view/palette.go:33`, replace:

```go
	{"/sessions", "сохранённые сессии"},
```

with:

```go
	{"/sessions", "сохранённые сессии · /sessions <текст> — поиск по сообщениям"},
```

In `ui/tui/view/palette_modal.go:31`, replace:

```go
	{"/sessions", "прошлые сессии", "Session"},
```

with:

```go
	{"/sessions", "прошлые сессии · /sessions <текст> — поиск по сообщениям", "Session"},
```

- [ ] **Step 6: Build, vet, full suite**

Run: `go build ./... && go vet ./...`, then `go test ./...`, then `go test -race ./ui/tui/...`
Expected: all green.

- [ ] **Step 7: Commit**

```bash
git add ui/tui/app_session.go ui/tui/app_palette.go ui/tui/app_session_search_test.go ui/tui/view/palette.go ui/tui/view/palette_modal.go
git commit -m "feat(tui): filter the session picker by message text with /sessions <query>"
```

---

## Task 7: TUI `/fork`

This task also fixes a defect in the neighbouring code it reuses.
`handleRewindDialog` (`ui/tui/app_dialogs.go:225-231`) calls
`a.handleRewindSelect(m.Checkpoint)` and **discards the returned `tea.Cmd`**,
returning `a, nil`. That command is what performs the `session.rewind` RPC and
the session persist, so rewinding through the dialog currently truncates the
local view without ever telling core or disk. The fork path returns a command
too, so it would inherit exactly the same bug.

**Files:**
- Modify: `ui/tui/view/dialog_rewind.go` (a fork variant of the checkpoint dialog)
- Modify: `ui/tui/view/dialog_msgs.go:77-81` (`RewindDialogMsg.Fork`)
- Modify: `ui/tui/app_dialogs.go:225-231` (dispatch + the dropped-command fix)
- Modify: `ui/tui/app_palette.go` (`/fork` case)
- Modify: `ui/tui/app_rewind.go` (fork flow)
- Modify: `ui/tui/view/palette.go`, `ui/tui/view/palette_modal.go` (palette entry)
- Create: `ui/tui/app_fork_test.go`

**Interfaces:**
- Consumes: `(*rpcclient.Client).SessionFork` and the `coreClient` interface entry (Task 4).

- [ ] **Step 1: Write the failing test**

Create `ui/tui/app_fork_test.go`:

```go
package tui

import (
	"testing"

	"github.com/orchestra/orchestra/ui/tui/state"
	"github.com/orchestra/orchestra/ui/tui/view"
)

func TestForkCheckpoints_ListsUserMessages(t *testing.T) {
	a := testChromeApp(t)
	a.session.AppendMessage(state.Message{Role: state.RoleUser, Text: "alpha"})
	a.session.AppendMessage(state.Message{Role: state.RoleAssistant, Text: "beta"})
	a.session.AppendMessage(state.Message{Role: state.RoleUser, Text: "gamma"})

	// Fork reuses rewind's checkpoint list: both branch at user turns.
	cps := a.rewindCheckpoints()
	if len(cps) != 2 {
		t.Fatalf("want 2 checkpoints, got %d", len(cps))
	}
	if cps[1].MsgIndex != 2 {
		t.Fatalf("second checkpoint index = %d, want 2", cps[1].MsgIndex)
	}
}

func TestHandleRewindDialog_ForwardsTheCommand(t *testing.T) {
	// Regression: the dialog handler used to discard the tea.Cmd returned by
	// handleRewindSelect, so picking a checkpoint truncated the local view but
	// never sent session.rewind to core or persisted the result.
	a := testChromeApp(t)
	a.session.AppendMessage(state.Message{Role: state.RoleUser, Text: "one"})
	a.session.AppendMessage(state.Message{Role: state.RoleAssistant, Text: "a1"})

	_, cmd := a.handleRewindDialog(view.RewindDialogMsg{
		Checkpoint: view.RewindCheckpoint{MsgIndex: 0, Label: "one"},
	})
	if cmd == nil {
		t.Fatal("picking a checkpoint must return the command that persists and notifies core")
	}
}

func TestHandleRewindDialog_ForkBranchIsRoutedToFork(t *testing.T) {
	a := testChromeApp(t)
	a.session.AppendMessage(state.Message{Role: state.RoleUser, Text: "one"})
	a.session.AppendMessage(state.Message{Role: state.RoleAssistant, Text: "a1"})
	a.session.AppendMessage(state.Message{Role: state.RoleUser, Text: "two"})

	// A fork must not truncate the current view — that is rewind's job.
	_, cmd := a.handleRewindDialog(view.RewindDialogMsg{
		Fork:       true,
		Checkpoint: view.RewindCheckpoint{MsgIndex: 2, Label: "two"},
	})
	if cmd == nil {
		t.Fatal("fork must return a command")
	}
	if len(a.session.Messages) != 3 {
		t.Fatalf("fork must leave the current session untouched, got %d messages", len(a.session.Messages))
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./ui/tui/... -run "Fork|HandleRewindDialog" -v`
Expected: FAIL — `RewindDialogMsg` has no field `Fork`, and `TestHandleRewindDialog_ForwardsTheCommand` fails with "picking a checkpoint must return the command…" because the handler currently returns nil.

- [ ] **Step 3: Add the fork flavour to the checkpoint dialog**

In `ui/tui/view/dialog_msgs.go`, extend `RewindDialogMsg` (lines 77-81):

```go
// RewindDialogMsg — checkpoint picker: cancel, or act on a checkpoint. Fork
// distinguishes the two actions the same picker serves: rewind truncates this
// session, fork branches into a new one.
type RewindDialogMsg struct {
	Cancel     bool
	Fork       bool
	Checkpoint RewindCheckpoint
}
```

In `ui/tui/view/dialog_rewind.go`, add the flag to the struct (lines 17-22) and a constructor beside `NewRewindDialog` (line 24):

```go
// RewindDialog lists user-message checkpoints for history rewind, and — with
// fork set — for branching into a new session instead.
type RewindDialog struct {
	items   []RewindCheckpoint
	cursor  int
	scroll  int
	screenH int
	fork    bool
}

func NewRewindDialog(items []RewindCheckpoint) *RewindDialog {
	return &RewindDialog{items: items, screenH: 24}
}

// NewForkDialog is the same checkpoint picker, labelled and tagged for fork.
func NewForkDialog(items []RewindCheckpoint) *RewindDialog {
	return &RewindDialog{items: items, screenH: 24, fork: true}
}
```

Tag both result paths in `Update` (lines 40 and 53-56):

```go
		case "esc":
			return d, resultCmd(RewindDialogMsg{Cancel: true, Fork: d.fork})
```

```go
		case "enter":
			if len(d.items) == 0 {
				return d, resultCmd(RewindDialogMsg{Cancel: true, Fork: d.fork})
			}
			cp := d.items[d.cursor]
			return d, resultCmd(RewindDialogMsg{Checkpoint: cp, Fork: d.fork})
```

And label it in `Render` — replace the title/hint block (lines 96-104):

```go
	verb := "Rewind"
	hint := "Enter — rewind · Esc — отмена"
	if d.fork {
		verb = "Fork"
		hint = "Enter — ветка · Esc — отмена"
	}
	title := verb + " checkpoint"
	if len(d.items) == 0 {
		title = verb + " — нет сообщений"
		hint = "Esc — закрыть"
	} else if len(d.items) > mv {
		title = fmt.Sprintf("%s  %d/%d", verb, d.cursor+1, len(d.items))
	}
```

- [ ] **Step 4: Write the fork flow**

In `ui/tui/app_rewind.go`, add at the end of the file:

```go
type sessionForkResultMsg struct {
	label string
	id    string
	err   error
}

func (a *App) openForkDialog() {
	if a.turn.ShowBusySpinner() {
		a.showToast("дождитесь конца хода")
		return
	}
	if a.rpc == nil || a.coreSessionID == "" {
		a.showToast("fork требует активную сессию ядра")
		return
	}
	a.showWelcome = false
	a.chat.SetForceWelcome(false)
	items := a.rewindCheckpoints()
	// The first user message cannot be a fork point: the branch would be empty.
	if len(items) > 0 && items[0].MsgIndex == 0 {
		items = items[1:]
	}
	if len(items) == 0 {
		a.showToast("нет точек для ветки")
		return
	}
	a.pushDialog(view.NewForkDialog(items))
}

// forkAtCheckpointCmd branches the session at cp without touching the current
// one — the difference from rewind, which truncates in place.
func (a *App) forkAtCheckpointCmd(cp view.RewindCheckpoint) tea.Cmd {
	if a.turn.ShowBusySpinner() {
		a.showToast("дождитесь конца хода")
		return nil
	}
	if a.rpc == nil || a.coreSessionID == "" {
		a.showToast("fork требует активную сессию ядра")
		return nil
	}
	idx := cp.MsgIndex
	msgs := a.session.Messages
	if idx <= 0 || idx >= len(msgs) || msgs[idx].Role != state.RoleUser {
		a.showToast("некорректная точка ветки")
		return nil
	}

	label := strings.TrimSpace(cp.Label)
	sid := a.coreSessionID
	rpc := a.rpc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		res, err := rpc.SessionFork(ctx, sid, idx)
		if err != nil {
			return sessionForkResultMsg{label: label, err: err}
		}
		return sessionForkResultMsg{label: label, id: res.SessionID}
	}
}

func (a *App) handleSessionForkResult(m sessionForkResultMsg) tea.Cmd {
	if m.err != nil {
		a.session.AppendSystemNotice(state.SystemKindError, "fork: "+m.err.Error())
		a.chat.SetMessages(a.session.Messages)
		return nil
	}
	a.showToast("ветка · " + m.label)
	// Switching in is the point of "try step N differently": the parent stays
	// on disk and is reachable from /sessions.
	return a.loadSession(m.id)
}
```

In `ui/tui/app_dialogs.go`, replace `handleRewindDialog` (lines 224-231) with:

```go
// handleRewindDialog — checkpoint picked in the rewind or fork dialog.
func (a *App) handleRewindDialog(m view.RewindDialogMsg) (tea.Model, tea.Cmd) {
	a.popDialog()
	if m.Cancel {
		return a, nil
	}
	// The returned command is what actually reaches core and persists; dropping
	// it silently truncated the view and told nobody.
	if m.Fork {
		return a, a.forkAtCheckpointCmd(m.Checkpoint)
	}
	return a, a.handleRewindSelect(m.Checkpoint)
}
```

In `ui/tui/app_update.go`, register the new result message next to
`sessionRewindResultMsg`'s case (search for `sessionRewindResultMsg` and mirror it):

```go
	case sessionForkResultMsg:
		return a, a.handleSessionForkResult(m)
```

In `ui/tui/app_palette.go`, add the command beside `/rewind` (lines 319-322):

```go
	case "/fork":
		dismissWelcome()
		a.openForkDialog()
		return nil
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./ui/tui/... -run "Fork|HandleRewindDialog" -v`
Expected: PASS, including the rewind dropped-command regression test.

- [ ] **Step 6: Register `/fork` in the palettes**

In `ui/tui/view/palette.go`, add directly after the `/rewind` entry at line 32
(`{"/rewind", "checkpoint rewind (скелет)"},`):

```go
	{"/fork", "ветка от сообщения (оригинал остаётся)"},
```

In `ui/tui/view/palette_modal.go`, add beside the `/sessions` entry at line 31,
using that file's three-field form with the "Session" category:

```go
	{"/fork", "ветка от сообщения (оригинал остаётся)", "Session"},
```

- [ ] **Step 7: Build, vet, full suite**

Run: `go build ./... && go vet ./...`, then `go test ./...`, then `go test -race ./ui/tui/...`
Expected: all green.

- [ ] **Step 8: Commit**

```bash
git add ui/tui/app_rewind.go ui/tui/app_dialogs.go ui/tui/app_update.go ui/tui/app_palette.go ui/tui/app_fork_test.go ui/tui/view/dialog_rewind.go ui/tui/view/dialog_msgs.go ui/tui/view/palette.go ui/tui/view/palette_modal.go
git commit -m "feat(tui): add /fork and fix the rewind dialog dropping its command"
```

---

## Task 8: Documentation

**Files:**
- Modify: `docs/parity-plan-2026-09.md` (§1.4 table and its "Есть" line)
- Modify: `README.md`, `README.ru.md`

- [ ] **Step 1: Close §1.4 #1 and #2 in the parity plan**

In `docs/parity-plan-2026-09.md`, replace the two rows of the §1.4 table:

```
| 1 | **Поиск по сессиям** (`orchestra session search`, `/sessions` с фильтром) — сейчас только список | M |
```

with:

```
| ~~1~~ | ~~**Поиск по сессиям**~~ — закрыто | `internal/sessionfile/search.go`: `Search` идёт по всем сессиям и возвращает попадания **на уровне сообщения** (`Hit.Index` — тот самый индекс, который берут `session fork --at` и `session.rewind`), со сниппетом; по умолчанию только текст сообщений, `--all` добавляет reasoning и tool-блоки, потому что вывод инструментов иначе топит прозу. Регистрозависимость по умолчанию и `-i` — как у соседней `orchestra search`. Индекса нет и не нужно: `ListMeta` и так парсит все файлы при каждом открытии пикера. `internal/cli/session.go`: `orchestra session search`. `ui/tui/app_session.go`: `/sessions <query>` засевает пикер отфильтрованным списком, сам диалог не изменился | M |
```

and:

```
| 2 | **Fork/branch от сообщения** — «попробовать иначе с шага 7» без потери ветки | M |
```

with:

```
| ~~2~~ | ~~**Fork/branch от сообщения**~~ — закрыто | `internal/sessionfile/fork.go`: `ForkSnapshot` копирует историю **до** выбранного user-сообщения (не включая его), поэтому ветка кончается ответом ассистента на предыдущий шаг и следующий промпт ложится чисто. Родословная — `parent_id`/`forked_from_index`, добавлены как additive-поля: схема осталась v4, потому что `LoadFromDisk` (`internal/core/session/persist.go:101-103`) жёстко отвергает чужую версию, и бамп сломал бы чтение старым бинарником. `internal/core/session_fork.go`: `session.fork` читает живую сессию из памяти (файл отстаёт до 5 с), родителя не трогает и ветку в Manager не регистрирует — её подхватит следующий `session.start`. Сессия после `/compact` форкнуться не может и получает явный отказ, а не тихо неверную ветку. `internal/cli/session.go`: `orchestra session fork --at`. `ui/tui/app_rewind.go`: `/fork` переиспользует диалог чекпоинтов и переключает в ветку. Попутно починен баг: `handleRewindDialog` терял возвращённый `tea.Cmd`, из-за чего rewind из диалога обрезал вид, но не доходил ни до ядра, ни до диска | M |
```

- [ ] **Step 2: Correct the section's inaccurate claim**

§1.4's "Есть" line credits an "авто-заголовок (`title.txt`)". Verified false:
`agent.ModeTitle` has no caller and `internal/prompt/files/title.txt` is dead
code; titles come from `TitleFromUIMessages`
(`internal/sessionfile/migrate.go:258-279`), a truncation of the first user
message. In that line, replace `авто-заголовок (`title.txt`)` with:

```
заголовок из первого user-сообщения (`sessionfile.TitleFromUIMessages`; LLM-заголовков нет — `agent.ModeTitle` не вызывается ниоткуда, промпт `title.txt` мёртв)
```

- [ ] **Step 3: Document the commands in both READMEs**

In `README.md`, find the session section (search for `orchestra session`) and add:

```markdown
`orchestra session search <query>` searches the text of every message in every
saved session and prints one line per match with its message index; `-i` makes
it case-insensitive and `--all` also searches reasoning and tool output.
`orchestra session fork <id> --at <index>` copies a session's history up to
that index into a new session, leaving the original untouched — unlike rewind,
which truncates in place. In the TUI, `/sessions <query>` filters the picker by
message text and `/fork` branches from a checkpoint.
```

In `README.ru.md`, add the same content in Russian at the matching place.

- [ ] **Step 4: Final verification**

Run: `go build ./... && go vet ./...`, then `go test ./...`, then
`go test -race ./internal/sessionfile/... ./internal/core/... ./internal/cli/... ./ui/tui/...`.
Then, in `llm/`, `patch/` and `protocol/`, run `go build ./... && go vet ./... && go test ./...`.
Expected: all green across all four modules.

- [ ] **Step 5: Commit**

```bash
git add docs/parity-plan-2026-09.md README.md README.ru.md
git commit -m "docs: close parity plan 1.4 #1-2 (session search and fork)"
```
