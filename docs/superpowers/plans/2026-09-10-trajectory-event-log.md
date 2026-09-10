# Trajectory Event Log (C2a) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The core records an append-only event log per session and serves it over JSON-RPC, so a trajectory view can be built against real, durable data.

**Architecture:** Every agent event already funnels through one place — the `notify` callback handed to `buildAgentOnEvent` in `internal/core/agent_launch.go`. Wrapping that callback tees each notification into a line-oriented sidecar file, `.orchestra/sessions/<id>.events.jsonl`, next to the snapshot it belongs to. The session snapshot schema does not change. A new `session.trajectory` method reads the log back, and `ProtocolVersion` moves to 16.

**Tech Stack:** Go 1.26, JSON Lines on disk, JSON-RPC 2.0 over stdio and WebSocket. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-09-09-multi-project-window-design.md` — the "The trajectory view (C2)" section, and the **C2** half of "Changes to the core".

**This plan is C2a of two.** It produces no user-visible change. C2b builds the Trajectory tab against what this plan records. The split is deliberate: a single plan covering both was 14-16 tasks, and C1 needed two fix rounds at nine.

## Global Constraints

- The event log is a **sidecar file** at `.orchestra/sessions/<id>.events.jsonl`, one JSON object per line. The session snapshot schema **stays at v4** — do not bump `sessionfile.Version`.
- The `.jsonl` extension is load-bearing: `sessionfile.ListMeta` treats every `.json` file in that directory as a session, so `<id>.events.json` would appear as a phantom session named `<id>.events`.
- `ProtocolVersion` moves **15 → 16**, and every client pinned to a version moves in the same commit. There is exactly one such client outside the Go module: `ui/vscode/src/coreSession.ts`.
- Every event carries a `source` field. Nothing in this plan populates it with anything but `"core"` — it exists so a plugin layer can fill it in later without a format migration.
- **Nothing synthesises a log.** A session with no sidecar reports that no log was recorded. Never reconstruct events from `ui_messages`.
- **No estimated numbers.** A token count that the provider did not report is absent, not zero.
- Timestamps are stamped by the core at emission — the clock where the work happens, never the client's.
- Errors wrap with `fmt.Errorf("...: %w", err)`. No panics for expected failures.
- All disk writes go through the existing helpers where they apply. The log is the one exception and it is explained in Task 2: an append cannot use `fsutil.AtomicWriteFile`, which rewrites a whole file.
- `go vet ./...` and `go test ./...` must pass. Windows is a first-class target: no assumption of `/` separators or a case-sensitive filesystem.
- **This plan may edit `ui/vscode/`.** C1's byte-identical rule bound C1 only; C2's acceptance condition is the opposite one. Do not carry C1's constraint forward.

---

### Task 1: Stop the loader losing transcripts, and guard the version drift

Two bugs found while reading this code before planning. Neither is C2's doing; both sit directly under it, and the second one makes Task 4 impossible until it is fixed.

**Bug A — a v2 or v3 session loses its entire chat transcript on load, silently.** `sessionfile.ParseSnapshot` sends any snapshot whose version is below the binary's own into `migrateV1`, which constructs its result with `UIMessages: nil`. `normalizeSnapshot` then stamps the current version onto that result, so the "unsupported snapshot version" guard in `internal/core/session/persist.go:112` never fires — the mangled snapshot is accepted as if it were healthy. Verified by experiment before this plan was written: a v2 and a v3 fixture carrying two `ui_messages` each both loaded with zero.

**Bug B — the VS Code extension cannot connect to the current core.** `ui/vscode/src/coreSession.ts:24` pins `PROTOCOL_VERSION = 14`; the core has been at 15 since the `/ws` transport landed. `coreSession.ts:1601-1609` throws on any mismatch *before* `initialize`, and its message tells the user to rebuild `orchestra.exe` — which points away from the real cause. No test anywhere compares the extension's constant against the core's, which is why v15 shipped without this being noticed. (A stale compiled `out/coreSession.js` in a developer's checkout carries the old number too, but `out/` is untracked build output and not this task's concern.)

**Files:**
- Modify: `internal/sessionfile/migrate.go` (the `probe.Version` dispatch)
- Modify: `internal/sessionfile/snapshot.go` (the `Snapshot` doc comment that misdescribes the loader)
- Modify: `internal/core/session/persist.go:112-114` and `:197-200` (the unreachable guards)
- Modify: `ui/vscode/src/coreSession.ts:24`
- Test: `internal/sessionfile/migrate_test.go`
- Test: `internal/cli/protocol_pin_test.go` (create)

**Interfaces:**
- Consumes: nothing.
- Produces: nothing new. `ParseSnapshot(data []byte, fileID string) (*Snapshot, error)` keeps its signature; only its dispatch changes.

- [ ] **Step 1: Write the failing test for the transcript loss**

Add to `internal/sessionfile/migrate_test.go`:

```go
func TestParseSnapshot_OlderSchemaKeepsUIMessages(t *testing.T) {
	// v2 introduced ui_messages; v3 and v4 only added fields to it. A file at
	// any of those versions therefore already has the modern shape and must be
	// parsed, not migrated. Routing it through the v1 migration returns
	// UIMessages: nil and loses the whole transcript.
	for _, ver := range []int{2, 3, 4} {
		data := []byte(fmt.Sprintf(`{
			"version": %d,
			"id": "s1",
			"created_at": "2026-01-01T00:00:00Z",
			"updated_at": "2026-01-01T00:00:00Z",
			"history": [{"role":"user","content":"hello"}],
			"ui_messages": [
				{"role":"user","text":"hello"},
				{"role":"assistant","text":"hi"}
			]
		}`, ver))
		snap, err := ParseSnapshot(data, "s1")
		if err != nil {
			t.Fatalf("v%d: ParseSnapshot: %v", ver, err)
		}
		if len(snap.UIMessages) != 2 {
			t.Errorf("v%d: ui_messages = %d, want 2 (transcript was discarded)", ver, len(snap.UIMessages))
		}
		if len(snap.History) != 1 {
			t.Errorf("v%d: history = %d, want 1", ver, len(snap.History))
		}
	}
}

func TestParseSnapshot_V1StillMigrates(t *testing.T) {
	// The floor must not be lowered so far that a genuine v1 file (history
	// only, no ui_messages) stops being migrated.
	data := []byte(`{
		"version": 1,
		"id": "s1",
		"created_at": "2026-01-01T00:00:00Z",
		"history": [{"role":"user","content":"hello"}]
	}`)
	snap, err := ParseSnapshot(data, "s1")
	if err != nil {
		t.Fatalf("ParseSnapshot: %v", err)
	}
	if len(snap.History) != 1 {
		t.Errorf("history = %d, want 1", len(snap.History))
	}
	if len(snap.UIMessages) != 0 {
		t.Errorf("ui_messages = %d, want 0 — v1 had none to carry", len(snap.UIMessages))
	}
}
```

`fmt` may not be imported in that file yet; add it if the compiler asks.

- [ ] **Step 2: Run it and watch it fail**

```bash
go test ./internal/sessionfile -run TestParseSnapshot_OlderSchemaKeeps -v
```

Expected: FAIL, twice — `v2: ui_messages = 0, want 2` and `v3: ui_messages = 0, want 2`. The `v4` case passes already. If v2 and v3 pass, stop and report: the bug this task exists to fix is not present and the rest of the task needs rethinking.

- [ ] **Step 3: Fix the dispatch**

In `internal/sessionfile/migrate.go`, replace:

```go
	if probe.Version >= Version {
```

with:

```go
	// v2 is the floor for "already the modern shape": it introduced
	// ui_messages, and v3 (segments) and v4 (attachments) only added fields
	// inside it, so a v2 or v3 file unmarshals into the current Snapshot with
	// the newer fields left zero. Comparing against Version instead sent every
	// older-but-modern file into migrateV1, which returns UIMessages: nil and
	// silently discarded the entire chat transcript. A file from a *newer*
	// binary also lands here and keeps working, because json.Unmarshal ignores
	// fields this build does not know.
	if probe.Version >= 2 {
```

- [ ] **Step 4: Run the tests and watch them pass**

```bash
go test ./internal/sessionfile -v
```

Expected: PASS, including the existing migration tests. If an existing test now fails, do not adjust it to match — report it, because it would mean some caller depends on the transcript being dropped.

- [ ] **Step 5: Correct the comment that caused this**

In `internal/sessionfile/snapshot.go`, the `Snapshot` doc comment currently claims a schema bump would make files unreadable by an older binary and cites a guard in `internal/core/session/persist.go`. That guard cannot fire — `normalizeSnapshot` stamps `s.Version = Version` on every load before any caller sees it — and this comment is why the schema has been frozen at v4 with fields bolted on additively. Replace the sentence beginning "Additive with omitempty on purpose:" (in the `ParentID` block) with:

```go
	// Additive with omitempty on purpose: a field an older binary does not
	// know is simply ignored by json.Unmarshal, so adding one costs nothing.
	//
	// Note on the version guards in internal/core/session/persist.go: they are
	// unreachable. ParseSnapshot always routes through normalizeSnapshot, which
	// sets Version to this binary's own before returning, so a loaded snapshot
	// can never disagree with it. An earlier version of this comment described
	// those guards as a working safety net and discouraged schema changes on
	// that basis; they were removed rather than left looking load-bearing.
```

Then delete the unreachable guards. In `internal/core/session/persist.go`, remove:

```go
	if snap.Version != sessionfile.Version {
		return nil, fmt.Errorf("session %s: unsupported snapshot version %d (this binary expects %d)", id, snap.Version, sessionfile.Version)
	}
```

and in the later function change:

```go
	if err != nil || snap.Version != sessionfile.Version {
		return false
	}
```

to:

```go
	if err != nil {
		return false
	}
```

If removing them leaves `fmt` or `sessionfile` unused in that file, the compiler will say so; fix the imports it names and nothing else.

- [ ] **Step 6: Write the failing test for the version drift**

Create `internal/cli/protocol_pin_test.go`. It reads the extension's TypeScript source and compares its pinned constant against the Go one, because nothing else in the repository does:

```go
package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/orchestra/orchestra/protocol"
)

// The VS Code extension pins its own copy of the protocol version and throws
// on any mismatch with core.health *before* initialize
// (ui/vscode/src/coreSession.ts). Nothing else compares the two, which is how
// the extension came to sit at 14 against a core at 15 — unable to connect at
// all, with an error message blaming orchestra.exe. This test is the guard
// that was missing.
func TestVSCodeExtensionPinsCurrentProtocolVersion(t *testing.T) {
	// Only the TypeScript source is checked: ui/vscode/out/ is untracked local
	// build output, absent in a fresh clone and in CI, so a test that read it
	// would fail everywhere the extension has not been compiled.
	rel := filepath.Join("..", "..", "ui", "vscode", "src", "coreSession.ts")
	data, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	m := regexp.MustCompile(`PROTOCOL_VERSION\s*=\s*(\d+)`).FindSubmatch(data)
	if m == nil {
		t.Fatalf("%s: no PROTOCOL_VERSION assignment found", rel)
	}
	got, err := strconv.Atoi(string(m[1]))
	if err != nil {
		t.Fatalf("%s: PROTOCOL_VERSION is not a number: %v", rel, err)
	}
	if got != protocol.ProtocolVersion {
		t.Errorf("%s pins PROTOCOL_VERSION = %d, core is %d — the extension "+
			"refuses to connect on mismatch, so these must move together",
			rel, got, protocol.ProtocolVersion)
	}
}
```

- [ ] **Step 7: Run it and watch it fail**

```bash
go test ./internal/cli -run TestVSCodeExtensionPinsCurrentProtocolVersion -v
```

Expected: FAIL, reporting `pins PROTOCOL_VERSION = 14, core is 15`.

- [ ] **Step 8: Sync the extension to the core's current version**

In `ui/vscode/src/coreSession.ts`, line 24:

```ts
const PROTOCOL_VERSION = 15;
```

That is the whole change. **Do not rebuild the extension and do not touch `ui/vscode/out/`** — it is untracked local build output (`git ls-files ui/vscode/out` is empty), absent in this worktree, and regenerated by whoever packages the extension. `ui/vscode/node_modules` is untracked too, so `npm run compile` would need an install first and would also regenerate the webview bundles, churning files this task has no business touching.

While you are here, improve the error message at `coreSession.ts:1605-1608`, which currently sends the reader to the wrong place:

```ts
        throw new Error(
          `protocol_version mismatch: extension=${PROTOCOL_VERSION}, core=${preHealth.protocol_version}. ` +
            `Whichever is older is the one to update — the extension's constant lives in ` +
            `src/coreSession.ts, the core's in protocol/version.go. Both must move together.`
        );
```

- [ ] **Step 9: Run everything and commit**

```bash
go vet ./... && go test ./...
```

Expected: all green, including both new tests.

```bash
git add internal/sessionfile internal/core/session/persist.go internal/cli/protocol_pin_test.go ui/vscode/src/coreSession.ts
git commit -m "fix: a v2 or v3 session no longer loses its transcript on load

ParseSnapshot compared the file's version against the binary's own and
sent anything older into the v1 migration, which returns UIMessages: nil.
A session written at schema v2 or v3 therefore loaded with an empty
transcript, and normalizeSnapshot stamped the current version on so the
guard meant to catch it could never fire. v2 is the real floor for the
modern shape; compare against that.

Removes those guards rather than leaving them looking load-bearing, and
corrects the Snapshot comment that described them as working — that
comment is why the schema has been frozen at v4.

Also syncs the VS Code extension's pinned protocol version from 14 to
the core's 15. It refuses to connect on mismatch before initialize, so
the extension could not talk to the current core at all, and its error
message blamed orchestra.exe. Adds the test that would have caught it."
```

---

### Task 2: The event record and the sidecar log

**Files:**
- Create: `internal/trajectory/event.go`
- Create: `internal/trajectory/log.go`
- Create: `internal/trajectory/log_test.go`
- Modify: `internal/sessionfile/store.go` (`Delete` must remove the sidecar)
- Test: `internal/sessionfile/store_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Event struct { Seq int64; TimeMS int64; Type string; Source string; Data json.RawMessage }`
  - `func Path(workspaceRoot, sessionID string) string`
  - `func NewWriter(workspaceRoot, sessionID string) (*Writer, error)`
  - `func (w *Writer) Append(eventType string, data any) error`
  - `func (w *Writer) Close() error`
  - `func Read(workspaceRoot, sessionID string) ([]Event, bool, error)` — the bool is `recorded`: false means no sidecar exists, which is different from a sidecar with no events.
  - `func SidecarPath(workspaceRoot, sessionID string) string` is **not** a second name for `Path`; only `Path` exists. Task 4 consumes `Read`; Task 3 consumes `NewWriter`/`Append`/`Close`.

- [ ] **Step 1: Write the failing tests**

Create `internal/trajectory/log_test.go`:

```go
package trajectory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAppendAndRead_RoundTripsWithContiguousSeq(t *testing.T) {
	root := t.TempDir()
	w, err := NewWriter(root, "s1")
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := w.Append("agent/event", map[string]any{"n": i}); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	events, recorded, err := Read(root, "s1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !recorded {
		t.Fatal("recorded = false, want true — the sidecar exists")
	}
	if len(events) != 3 {
		t.Fatalf("len(events) = %d, want 3", len(events))
	}
	for i, ev := range events {
		if ev.Seq != int64(i+1) {
			t.Errorf("events[%d].Seq = %d, want %d — sequence must be contiguous from 1", i, ev.Seq, i+1)
		}
		if ev.Type != "agent/event" {
			t.Errorf("events[%d].Type = %q, want %q", i, ev.Type, "agent/event")
		}
		if ev.Source != "core" {
			t.Errorf("events[%d].Source = %q, want %q", i, ev.Source, "core")
		}
		if ev.TimeMS <= 0 {
			t.Errorf("events[%d].TimeMS = %d, want a real timestamp", i, ev.TimeMS)
		}
	}
}

func TestRead_MissingSidecarIsNotRecordedRatherThanEmpty(t *testing.T) {
	root := t.TempDir()
	events, recorded, err := Read(root, "never-ran")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if recorded {
		t.Error("recorded = true, want false — there is no sidecar for this session")
	}
	if len(events) != 0 {
		t.Errorf("len(events) = %d, want 0", len(events))
	}
}

func TestRead_TornTrailingLineKeepsEveryCompleteEventBeforeIt(t *testing.T) {
	root := t.TempDir()
	w, err := NewWriter(root, "s1")
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := w.Append("agent/event", map[string]any{"n": i}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Simulate a crash mid-write: a partial third line with no newline.
	f, err := os.OpenFile(Path(root, "s1"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open for torn write: %v", err)
	}
	if _, err := f.WriteString(`{"seq":3,"type":"agent/ev`); err != nil {
		t.Fatalf("torn write: %v", err)
	}
	_ = f.Close()

	events, recorded, err := Read(root, "s1")
	if err != nil {
		t.Fatalf("Read must not fail on a torn trailing line: %v", err)
	}
	if !recorded {
		t.Error("recorded = false, want true")
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2 — the torn line is dropped, the complete ones survive", len(events))
	}
}

func TestAppend_ContinuesTheSequenceAcrossWriters(t *testing.T) {
	root := t.TempDir()
	w1, err := NewWriter(root, "s1")
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w1.Append("a", nil); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := w1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// A second turn in the same session opens a new writer. The sequence must
	// continue rather than restart, or two events share a seq and the log
	// stops being ordered.
	w2, err := NewWriter(root, "s1")
	if err != nil {
		t.Fatalf("NewWriter again: %v", err)
	}
	if err := w2.Append("b", nil); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := w2.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	events, _, err := Read(root, "s1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	if events[0].Seq != 1 || events[1].Seq != 2 {
		t.Errorf("seqs = %d,%d — want 1,2", events[0].Seq, events[1].Seq)
	}
}

func TestPath_UsesJSONLSoItIsNotMistakenForASession(t *testing.T) {
	// sessionfile.ListMeta treats every .json file in the sessions directory
	// as a session. A sidecar ending in .json would show up in the session
	// list as a phantom session named "<id>.events".
	got := filepath.Base(Path(t.TempDir(), "s1"))
	if got != "s1.events.jsonl" {
		t.Errorf("Path base = %q, want %q", got, "s1.events.jsonl")
	}
}

func TestAppend_DataIsStoredAsGivenNotStringified(t *testing.T) {
	root := t.TempDir()
	w, _ := NewWriter(root, "s1")
	if err := w.Append("agent/event", map[string]any{"type": "tool_call_start", "step": 2}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	_ = w.Close()

	events, _, err := Read(root, "s1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var payload struct {
		Type string `json:"type"`
		Step int    `json:"step"`
	}
	if err := json.Unmarshal(events[0].Data, &payload); err != nil {
		t.Fatalf("Data is not a JSON object: %v (raw: %s)", err, events[0].Data)
	}
	if payload.Type != "tool_call_start" || payload.Step != 2 {
		t.Errorf("payload = %+v, want {tool_call_start 2}", payload)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

```bash
go test ./internal/trajectory
```

Expected: FAIL to build — `no Go files in .../internal/trajectory`, or undefined `NewWriter`/`Read`/`Path`.

- [ ] **Step 3: Write the event record**

Create `internal/trajectory/event.go`:

```go
// Package trajectory records what an agent turn did, as an append-only log.
//
// One file per session, one JSON object per line, beside the session snapshot
// it belongs to. The snapshot is a single document rewritten whole on every
// save, so a log kept inside it would rewrite the entire history on every
// event; and a torn write there loses the session, where a torn line here
// costs at most that one line.
package trajectory

import "encoding/json"

// Event is one recorded thing. The field names are the ones a derived
// projection will need later — see the spec's "One event shape, defined once".
type Event struct {
	// Seq is monotonic and strictly increasing from 1 within a session. It is
	// contiguous in normal operation; a gap means an event failed to be
	// written, which is left visible on purpose — reusing a number would risk
	// two different events sharing it.
	Seq int64 `json:"seq"`
	// TimeMS is epoch milliseconds, stamped by the core when the event
	// happened. Never a client's clock: durations are measured where the work
	// runs, not where it is observed.
	TimeMS int64 `json:"time_ms"`
	// Type is the notification method that carried this event —
	// "agent/event", "exec/output_chunk", and so on.
	Type string `json:"type"`
	// Source names what produced the event. Always "core" today. It exists so
	// that a plugin layer can fill it in without a format migration; see the
	// spec's "Where this meets extensibility".
	Source string `json:"source"`
	// Data is the notification's params, stored as they were sent.
	Data json.RawMessage `json:"data,omitempty"`
}

// SourceCore is the only source this build emits.
const SourceCore = "core"
```

- [ ] **Step 4: Write the log**

Create `internal/trajectory/log.go`:

```go
package trajectory

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Path returns the sidecar's location for a session.
//
// The .jsonl extension is not cosmetic: sessionfile.ListMeta treats every
// .json file in this directory as a session, so a sidecar named
// "<id>.events.json" would appear in the session list as a phantom session
// called "<id>.events".
func Path(workspaceRoot, sessionID string) string {
	return filepath.Join(workspaceRoot, ".orchestra", "sessions", sessionID+".events.jsonl")
}

// Writer appends events to one session's log.
type Writer struct {
	mu   sync.Mutex
	f    *os.File
	seq  int64
	path string
	// tornWrite is set when a write failed and may have left a partial line.
	// The next append starts on a fresh line so it cannot merge into it.
	tornWrite bool
}

// NewWriter opens a session's log for appending, continuing its sequence.
//
// One writer per session at a time. Two concurrent writers would each read the
// same on-disk maximum and hand out the same next seq; nothing here prevents
// that, because the core already serialises turns within a session. A caller
// that ever runs two turns against one session must serialise them itself.
func NewWriter(workspaceRoot, sessionID string) (*Writer, error) {
	if workspaceRoot == "" || sessionID == "" {
		return nil, fmt.Errorf("trajectory: workspace_root and session_id required")
	}
	p := Path(workspaceRoot, sessionID)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return nil, fmt.Errorf("trajectory: mkdir: %w", err)
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("trajectory: open %s: %w", p, err)
	}
	// Repair a torn tail before anything else. A crash can stop a write
	// anywhere, including between a line's last byte and its newline, so the
	// file may end mid-line. Appending onto that merges the fragment and the
	// next event into one unparseable line — losing both — and lastSeq would
	// then reissue the fragment's number to different content.
	//
	// Terminating the fragment rather than truncating it is deliberate: a
	// write interrupted just before its newline leaves a COMPLETE, parseable
	// event, and truncation would throw that away. One newline destroys
	// nothing, and an append-only log has no business rewriting its history.
	if err := terminateTornTail(f, p); err != nil {
		_ = f.Close()
		return nil, err
	}
	// After the repair, so a recovered final line counts toward the sequence.
	// The sequence continues across turns: restarting at 1 would give two
	// events the same seq and the log would stop being ordered.
	last, err := lastSeq(p)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return &Writer{f: f, seq: last, path: p}, nil
}

// terminateTornTail ends the file with a newline if it does not already, so
// the next append starts on a line of its own.
func terminateTornTail(f *os.File, path string) error {
	st, err := f.Stat()
	if err != nil {
		return fmt.Errorf("trajectory: stat %s: %w", path, err)
	}
	if st.Size() == 0 {
		return nil
	}
	r, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("trajectory: reopen %s: %w", path, err)
	}
	defer func() { _ = r.Close() }()
	var b [1]byte
	if _, err := r.ReadAt(b[:], st.Size()-1); err != nil {
		return fmt.Errorf("trajectory: read tail of %s: %w", path, err)
	}
	if b[0] == '\n' {
		return nil
	}
	if _, err := f.Write([]byte{'\n'}); err != nil {
		return fmt.Errorf("trajectory: terminate torn tail of %s: %w", path, err)
	}
	return nil
}

// Append writes one event. data is stored as sent; nil is stored as no payload.
func (w *Writer) Append(eventType string, data any) error {
	if w == nil {
		return nil
	}
	var raw json.RawMessage
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("trajectory: marshal payload: %w", err)
		}
		raw = b
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	w.seq++
	line, err := json.Marshal(Event{
		Seq:    w.seq,
		TimeMS: time.Now().UTC().UnixMilli(),
		Type:   eventType,
		Source: SourceCore,
		Data:   raw,
	})
	if err != nil {
		// Safe to roll back: nothing has reached the file yet.
		w.seq--
		return fmt.Errorf("trajectory: marshal event: %w", err)
	}
	buf := append(line, '\n')
	if w.tornWrite {
		// A previous write failed and may have left a partial line. Start on a
		// line of our own rather than merging into it.
		buf = append([]byte{'\n'}, buf...)
	}
	// One write for the whole line: a single short write is what makes a torn
	// tail a torn *line*, which NewWriter terminates and Read skips.
	if _, err := w.f.Write(buf); err != nil {
		// Deliberately no rollback here, unlike the marshal failure above: a
		// failed write may have left a partial line, so reusing this number
		// could give two different events the same seq. A gap is visible and
		// harmless; a duplicate is silent corruption.
		w.tornWrite = true
		return fmt.Errorf("trajectory: append to %s: %w", w.path, err)
	}
	w.tornWrite = false
	return nil
}

// Close releases the file. Appends after Close fail.
func (w *Writer) Close() error {
	if w == nil || w.f == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	err := w.f.Close()
	w.f = nil
	if err != nil {
		return fmt.Errorf("trajectory: close %s: %w", w.path, err)
	}
	return nil
}

// Read returns a session's events. recorded is false when no log exists for
// the session at all, which is a different answer from a log with no events:
// one means "this session predates the log", the other "nothing happened yet".
func Read(workspaceRoot, sessionID string) (events []Event, recorded bool, err error) {
	p := Path(workspaceRoot, sessionID)
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("trajectory: open %s: %w", p, err)
	}
	defer func() { _ = f.Close() }()

	out := []Event{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			// Any line that will not parse is skipped, wherever it sits — not
			// only a torn tail. That is deliberate: one unreadable line must
			// not deny a reader the rest of the log, and a log outlives the
			// code that reads it. The cost is that a corrupt line in the
			// middle of a file is as quiet as an expected one at the end.
			continue
		}
		out = append(out, ev)
	}
	if err := sc.Err(); err != nil {
		return nil, true, fmt.Errorf("trajectory: read %s: %w", p, err)
	}
	return out, true, nil
}

func lastSeq(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("trajectory: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var last int64
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		var ev Event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			continue
		}
		if ev.Seq > last {
			last = ev.Seq
		}
	}
	if err := sc.Err(); err != nil {
		return 0, fmt.Errorf("trajectory: scan %s: %w", path, err)
	}
	return last, nil
}
```

- [ ] **Step 5: Run the tests and watch them pass**

```bash
go test ./internal/trajectory -v
```

Expected: all six PASS.

- [ ] **Step 6: Write the failing test for sidecar deletion**

Deleting a session must delete its log; `sessionfile.Delete` removes only the snapshot today, so the log would be orphaned and a new session reusing the id would inherit it. Add to `internal/sessionfile/store_test.go`:

```go
func TestDelete_RemovesTheTrajectorySidecar(t *testing.T) {
	root := t.TempDir()
	if err := Save(root, &Snapshot{ID: "s1"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	sidecar := filepath.Join(root, ".orchestra", "sessions", "s1.events.jsonl")
	if err := os.WriteFile(sidecar, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}

	if err := Delete(root, "s1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
		t.Errorf("sidecar still present after Delete (err = %v)", err)
	}
}
```

Add `os` and `path/filepath` to that file's imports if they are not there.

- [ ] **Step 7: Run it and watch it fail**

```bash
go test ./internal/sessionfile -run TestDelete_RemovesTheTrajectorySidecar -v
```

Expected: FAIL with `sidecar still present after Delete`.

- [ ] **Step 8: Delete the sidecar with the snapshot**

`internal/sessionfile` must not import `internal/trajectory` — the dependency runs the other way for every other package, and a cycle is easy to create here. Build the path locally instead. In `internal/sessionfile/store.go`, replace the body of `Delete`:

```go
// Delete removes a session file and its trajectory sidecar. Missing files are
// not an error.
//
// The sidecar's name is spelled out here rather than imported from
// internal/trajectory: that package is a consumer of session storage, and
// importing it back would invert the dependency. trajectory.Path is the same
// string, and internal/trajectory has a test that fails if the two drift.
func Delete(workspaceRoot, id string) error {
	if workspaceRoot == "" || id == "" {
		return nil
	}
	if err := os.Remove(snapshotPath(workspaceRoot, id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	sidecar := filepath.Join(sessionsDir(workspaceRoot), id+".events.jsonl")
	if err := os.Remove(sidecar); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("sessionfile: remove trajectory sidecar: %w", err)
	}
	return nil
}
```

- [ ] **Step 9: Add the test that keeps the two spellings from drifting**

Because the path is now written in two packages, add to `internal/trajectory/log_test.go`:

```go
func TestPath_MatchesTheNameSessionfileDeletes(t *testing.T) {
	// internal/sessionfile.Delete builds this same name by hand rather than
	// importing this package, to keep the dependency pointing one way. If the
	// two ever disagree, deleting a session silently orphans its log.
	root := t.TempDir()
	want := filepath.Join(root, ".orchestra", "sessions", "s1.events.jsonl")
	if got := Path(root, "s1"); got != want {
		t.Errorf("Path = %q, want %q — and internal/sessionfile.Delete must be updated with it", got, want)
	}
}
```

- [ ] **Step 10: Run everything and commit**

```bash
go vet ./... && go test ./...
```

```bash
git add internal/trajectory internal/sessionfile
git commit -m "feat(core): an append-only event log per session, as a sidecar

.orchestra/sessions/<id>.events.jsonl, one JSON object per line beside
the snapshot. A line-oriented sidecar gives append-only writes without
rewriting the snapshot on every event, and a torn trailing line costs one
event instead of the session. The .jsonl extension is load-bearing:
ListMeta treats every .json in that directory as a session.

Read distinguishes 'no log recorded' from 'a log with no events', because
the trajectory view has to say which. Nothing is ever synthesised.

Delete now removes the sidecar too, with a test on both sides that the
two spellings of the path cannot drift."
```

---

### Task 3: Record events as they are emitted

Every agent notification originates from one field: `spec.OnEvent`, handed to `prepareAgentLaunch`. Teeing that single field records exactly what the client sees — including `step_usage`, which already carries per-step token counts from the provider, so nothing new is needed for tokens.

**Tee `spec.OnEvent` once, not at each consumer.** An earlier draft of this task named two call sites, `:154` and `:236`. That was wrong: `spec.OnEvent` is consumed in *four* places — `buildAgentOnEvent` (:154), the `mode_route` notification (:185), `childCfg.NotifyAgentEvent` (:228), and `buildAgentOnEventWithChild` (:236). Wrapping two of them would have silently dropped mode-routing decisions and one of the two child-event paths from every log. Reassigning `spec.OnEvent` before its first use gives all four the tee for free, and there is nothing to keep in sync later.

Recording happens *after* debouncing, deliberately: `wrapStreamDebounce` coalesces text deltas, and the log wants the coalesced stream rather than every keystroke-sized chunk. The debouncer holds a pending delta behind a `time.Timer`, but `handle` flushes it synchronously on any non-delta event and a turn always ends with one, so the tail of a turn is on disk before the writer closes. Do not add a drain step for a race that the flush already closes.

**Files:**
- Modify: `internal/core/agent_launch.go` (one tee at the top of `prepareAgentLaunch`, plus a `Trajectory` field and a `Close` method on `agentLaunch`)
- Modify: `internal/core/core_agent.go:147`, `internal/core/session_rpc.go:418`, `internal/core/session_rpc.go:1072` — the three callers, each closing the launch
- Create: `internal/core/trajectory_tee.go`
- Test: `internal/core/trajectory_tee_test.go`

**Interfaces:**
- Consumes: `trajectory.NewWriter`, `(*trajectory.Writer).Append`, `(*trajectory.Writer).Close`, `trajectory.Read` (Task 2).
- Produces: `func teeToTrajectory(notify func(method string, params any), w *trajectory.Writer) func(string, any)` — used by Task 3 only; Task 4 reads through `trajectory.Read`.

- [ ] **Step 1: Write the failing test**

Create `internal/core/trajectory_tee_test.go`:

```go
package core

import (
	"encoding/json"
	"testing"

	"github.com/orchestra/orchestra/internal/trajectory"
)

func TestTeeToTrajectory_RecordsWhatItForwards(t *testing.T) {
	root := t.TempDir()
	w, err := trajectory.NewWriter(root, "s1")
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	var forwarded []string
	notify := func(method string, params any) { forwarded = append(forwarded, method) }

	tee := teeToTrajectory(notify, w)
	tee("agent/event", map[string]any{"type": "tool_call_start", "step": 1})
	tee("exec/output_chunk", map[string]any{"chunk": "hello"})

	// Forwarding must be unchanged — the tee is additive.
	if len(forwarded) != 2 || forwarded[0] != "agent/event" || forwarded[1] != "exec/output_chunk" {
		t.Fatalf("forwarded = %v, want [agent/event exec/output_chunk]", forwarded)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	events, recorded, err := trajectory.Read(root, "s1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !recorded || len(events) != 2 {
		t.Fatalf("recorded=%v len=%d, want true and 2", recorded, len(events))
	}
	if events[0].Type != "agent/event" || events[1].Type != "exec/output_chunk" {
		t.Errorf("types = %q,%q", events[0].Type, events[1].Type)
	}
	var first map[string]any
	if err := json.Unmarshal(events[0].Data, &first); err != nil {
		t.Fatalf("payload not stored as an object: %v", err)
	}
	if first["type"] != "tool_call_start" {
		t.Errorf("payload type = %v, want tool_call_start", first["type"])
	}
}

func TestTeeToTrajectory_NilWriterForwardsAndDoesNotPanic(t *testing.T) {
	// A session with no writer (a one-shot agent.run has no session id) must
	// keep notifying. Recording is best-effort; delivery is not.
	var forwarded int
	tee := teeToTrajectory(func(string, any) { forwarded++ }, nil)
	tee("agent/event", map[string]any{"type": "done"})
	if forwarded != 1 {
		t.Errorf("forwarded = %d, want 1", forwarded)
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
go test ./internal/core -run TestTeeToTrajectory -v
```

Expected: FAIL to compile — `undefined: teeToTrajectory`.

- [ ] **Step 3: Write the tee**

Create `internal/core/trajectory_tee.go`:

```go
package core

import "github.com/orchestra/orchestra/internal/trajectory"

// teeToTrajectory returns a notify function that records every notification
// into the session's event log before forwarding it.
//
// This is the one place every agent notification passes through, which is why
// the log hooks here instead of inside emitAgentStreamEvent: one wrapper
// records agent/event, exec/output_chunk and anything added later, and it
// records exactly what the client is told.
//
// Recording is best-effort. A log that cannot be written must never stop a
// turn from streaming — the user's work matters more than our observability —
// so a write error is dropped rather than propagated. There is no error path
// to propagate it down: notify returns nothing.
func teeToTrajectory(notify func(method string, params any), w *trajectory.Writer) func(string, any) {
	if notify == nil {
		notify = func(string, any) {}
	}
	if w == nil {
		return notify
	}
	return func(method string, params any) {
		_ = w.Append(method, params)
		notify(method, params)
	}
}
```

- [ ] **Step 4: Run the tests and watch them pass**

```bash
go test ./internal/core -run TestTeeToTrajectory -v
```

Expected: PASS.

- [ ] **Step 5: Wire it into the launch path**

Read `internal/core/agent_launch.go` from `prepareAgentLaunch` (line 113) to its `return` before editing, and read the three callers listed in **Files**.

**`prepareAgentLaunch` does not own the run.** It builds an `*agentLaunch` and returns it; the callers construct the agent and drive the turn. A `defer tw.Close()` inside `prepareAgentLaunch` would therefore close the log *before the first event is ever recorded*. The writer's lifetime belongs to the launch, so it travels on the launch and the callers close it.

Add the field to `agentLaunch` (the struct at line 59):

```go
type agentLaunch struct {
	Opts       agent.Options
	Custom     customAgentOpts
	Usage      *usage.Tracker
	TaskRunner *tasks.TaskRunner
	Profile    string

	RequestedMode   string
	EffectiveMode   string
	RouteReason     string
	RouteConfidence float64
	EventEnvelope   EventEnvelope

	// Trajectory records this turn's notifications. Nil when there is no
	// session to record against, or when the log could not be opened;
	// Close is safe either way.
	Trajectory *trajectory.Writer
}

// Close releases what the launch holds open. Callers own the turn, so they
// own this: prepareAgentLaunch returns before the first event exists and
// cannot defer it itself.
func (l *agentLaunch) Close() {
	if l == nil || l.Trajectory == nil {
		return
	}
	_ = l.Trajectory.Close()
}
```

Then, in `prepareAgentLaunch`, immediately after `env` is resolved (after the `if env.TurnID == "" { ... }` block at line 148) and **before** the `if spec.OnEvent != nil` at line 153, open the writer and tee:

```go
	// One tee for every consumer of spec.OnEvent below. There are four, and
	// wrapping them individually would drop whichever one a later change adds.
	var tw *trajectory.Writer
	if spec.SessionID != "" {
		w, err := trajectory.NewWriter(c.workspaceRoot, spec.SessionID)
		if err != nil {
			// Observability must never block work: carry on with no recorder
			// rather than failing the turn.
			fmt.Fprintf(os.Stderr, "core: session %s trajectory recording disabled: %v\n", spec.SessionID, err)
		} else {
			tw = w
			spec.OnEvent = teeToTrajectory(spec.OnEvent, tw)
		}
	}
```

Note what this deliberately does **not** guard on: `spec.OnEvent` may be nil, and the tee still installs. A core with no notifier attached — which is every test built through `setupInitializedCore`, and any headless embedding — has `spec.OnEvent == nil`, and a turn there deserves a trajectory just as much as one somebody is watching. `teeToTrajectory` substitutes a no-op notify for nil, so the log records and nothing is delivered.

This does flip four `if spec.OnEvent != nil` guards from false to true in that configuration, which is intended: the `mode_route` decision and the child-event sinks are exactly the things a trajectory should contain. The cost is a map allocation per event in a run nobody is watching. Accept it; do not reintroduce a nil guard to avoid it.

Add `"fmt"`, `"os"`, and `"github.com/orchestra/orchestra/internal/trajectory"` to the file's imports if they are not already there.

Finally, in each of the three callers, close the launch on every exit path. Immediately after the existing `if err != nil { return nil, err }` that follows `prepareAgentLaunch`:

```go
	defer launch.Close()
```

Add it at all three sites — `core_agent.go:147`, `session_rpc.go:418`, `session_rpc.go:1072`. It is a no-op at the `agent.run` and `session.compact` sites, where no session id reaches the spec and `Trajectory` stays nil; adding it anyway keeps one idiom rather than three special cases, and it is what makes a later `SessionID` on those specs record correctly instead of leaking a handle.

- [ ] **Step 6: Write the end-to-end test**

Add to `internal/core/trajectory_tee_test.go` a test that a real session turn leaves a log. The harness already exists and is named: `setupInitializedCore(t, root, &fixedLLM{steps: []string{...}})` in `internal/core/rpc_handler_test.go:150`, driven through `h.Handle(ctx, "session.start", ...)` then `h.Handle(ctx, "session.message", ...)`. Read `internal/core/session_history_persist_test.go` for a complete worked example of exactly this shape and follow it. Note that this harness attaches no notifier, so `p.OnEvent` is nil — which is precisely the configuration Step 5's tee must still record in; if this test finds no log, the bug is a nil guard in Step 5, not a fault in the harness. The assertion:

```go
func TestSessionTurn_LeavesATrajectoryOnDisk(t *testing.T) {
	// Drive one session.message turn against a scripted LLM, then assert the
	// sidecar exists and contains the turn's events in order.
	//
	// Build the core the way the neighbouring tests in this package do; do not
	// invent a new harness. Assert at least:
	//   - trajectory.Read(root, sessionID) returns recorded == true
	//   - the events include an "agent/event" whose payload is
	//     {"type":"step_done","content":"final"} — the terminal marker for
	//     this harness. NOT "done": fixedLLM has no Streamer, so the agent
	//     takes the non-streaming path and StreamEventDone/emitStepUsage
	//     never fire. See run_final.go:133.
	//   - every Seq is contiguous from 1
}
```

Write the real test body following the neighbouring pattern. Then prove it is load-bearing before you believe it: remove the tee, confirm the test fails, and put it back. A test that passes for the wrong reason is worse than a missing one here, because the on-disk format is the expensive thing to get wrong. If no existing test in `internal/core` drives a full session turn, say so in your report and assert at the closest reachable seam instead — do not leave this step as a comment.

- [ ] **Step 7: Run everything and commit**

```bash
go vet ./... && go test ./...
```

```bash
git add internal/core
git commit -m "feat(core): record every agent notification into the session log

Every notification already funnels through the notify callback handed to
buildAgentOnEvent, so the log tees there: one wrapper records
agent/event, exec/output_chunk and whatever is added later, and records
exactly what the client is told. Recording happens after debouncing on
purpose — the log wants the coalesced stream, not every chunk.

step_usage already carries per-step token counts from the provider, so
tokens need no new plumbing; they are simply recorded now.

Recording is best-effort: a log that cannot be opened or written disables
recording for that turn rather than failing it."
```

---

### Task 4: Serve the log over JSON-RPC, and move the protocol to 16

**Files:**
- Modify: `protocol/version.go` (the `ProtocolVersion` constant and its history comment)
- Create: `internal/core/trajectory_rpc.go`
- Modify: `internal/core/rpc_handler.go` (a case beside the other `session.*` methods)
- Modify: `ui/vscode/src/coreSession.ts:24` (the TypeScript source only — `ui/vscode/out/` is untracked build output; Step 7 says why)
- Modify: `docs/PROTOCOL.md`
- Test: `internal/core/trajectory_rpc_test.go`

**Interfaces:**
- Consumes: `trajectory.Read(workspaceRoot, sessionID) ([]trajectory.Event, bool, error)` (Task 2).
- Produces: the `session.trajectory` method. Params `{"session_id": string}`. Result:
  ```json
  { "recorded": true, "events": [ { "seq": 1, "time_ms": 1757500000000, "type": "agent/event", "source": "core", "data": { } } ] }
  ```
  `recorded: false` with an empty `events` means the session predates the log. C2b renders that as a sentence, not as an empty frame.

- [ ] **Step 1: Write the failing test**

Create `internal/core/trajectory_rpc_test.go`:

```go
package core

import (
	"testing"

	"github.com/orchestra/orchestra/internal/trajectory"
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
```

`setupInitializedCore(t, root, &fixedLLM{})` is the real helper, defined at `internal/core/rpc_handler_test.go:150`; it returns `(*Core, *RPCHandler)` and these tests need only the first. It writes an `.orchestra.yml`, disables LSP, builds the core and drives `initialize`, so the core it hands back is ready to serve. Do not add a helper of your own — an earlier draft of this task invented a `newTestCore` that does not exist anywhere in the repository, and transcribing it verbatim would not have compiled.

- [ ] **Step 2: Run it and watch it fail**

```bash
go test ./internal/core -run TestSessionTrajectory -v
```

Expected: FAIL to compile — `undefined: SessionTrajectoryParams`.

- [ ] **Step 3: Write the method**

Create `internal/core/trajectory_rpc.go`:

```go
package core

import (
	"strings"

	"github.com/orchestra/orchestra/internal/trajectory"
	"github.com/orchestra/orchestra/protocol"
)

// SessionTrajectoryParams selects the session whose log to read.
type SessionTrajectoryParams struct {
	SessionID string `json:"session_id"`
}

// SessionTrajectoryResult carries a session's recorded events.
//
// Recorded distinguishes "this session has no log" from "this session's log is
// empty". They look identical in the events array and mean different things to
// a reader: the first is a session that predates the feature, the second a
// session where nothing has happened yet. The view says which, in words.
type SessionTrajectoryResult struct {
	Recorded bool               `json:"recorded"`
	Events   []trajectory.Event `json:"events"`
}

// SessionTrajectory returns the append-only event log for a session.
//
// A session id that names nothing yields Recorded false rather than an error:
// the log is a sidecar, so "no log here" is the same answer for a session that
// predates the feature and for one that never existed, and this method has no
// business deciding which. Only a blank id is rejected, because that is a
// malformed request rather than a question about a session.
func (c *Core) SessionTrajectory(p SessionTrajectoryParams) (*SessionTrajectoryResult, error) {
	if c == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "core is nil", nil)
	}
	id := strings.TrimSpace(p.SessionID)
	if id == "" {
		return nil, protocol.NewError(protocol.InvalidParams, "session_id is empty", nil)
	}
	events, recorded, err := trajectory.Read(c.workspaceRoot, id)
	if err != nil {
		return nil, protocol.NewError(protocol.ExecFailed, err.Error(), map[string]any{"session_id": id})
	}
	// Never nil: `events` marshals to `null` when nil, and a client that reads
	// `events.length` would fault on it. An empty log is `[]`.
	if events == nil {
		events = []trajectory.Event{}
	}
	return &SessionTrajectoryResult{Recorded: recorded, Events: events}, nil
}
```

Three things here are not free choices, and an earlier draft of this task got each of them wrong:

- **The errors are `protocol.NewError`, not `fmt.Errorf`.** A plain error crosses the wire as a generic internal failure; `protocol.InvalidParams` is what tells a client it sent a bad request. `internal/core/message_attachments.go:104` is the established shape for exactly this case, an empty required string.
- **The result is a pointer, and error paths return `nil`.** Every neighbouring method does this — compare `SessionHistory` at `session_rpc.go:1023`. A value return would put a zero struct on the wire beside an error.
- **The workspace root is the field `c.workspaceRoot`.** There is no `WorkspaceRoot()` method on `Core`.


- [ ] **Step 4: Register the method**

In `internal/core/rpc_handler.go`, beside the other `session.*` cases (they run from about line 139 to 230), add one in the same shape as its neighbours:

```go
	case "session.trajectory":
		var p SessionTrajectoryParams
		if err := decodeParams(params, &p); err != nil {
			return nil, protocol.NewError(protocol.InvalidParams, "Invalid JSON format: "+err.Error(), map[string]any{
				"method": method,
			})
		}
		return h.core.SessionTrajectory(p)
```

- [ ] **Step 5: Run the tests and watch them pass**

```bash
go test ./internal/core -run TestSessionTrajectory -v
```

Expected: PASS.

- [ ] **Step 6: Move the protocol version, in one commit, everywhere**

In `protocol/version.go`, add a history line above the constant and change it:

```go
	// v16: session.trajectory — the append-only per-session event log
	//      (.orchestra/sessions/<id>.events.jsonl) read back as {recorded,
	//      events[]}. The on-disk session schema is unchanged and stays at v4.
	ProtocolVersion = 16
```

In `ui/vscode/src/coreSession.ts`, line 24:

```ts
const PROTOCOL_VERSION = 16;
```

`internal/cli/protocol_pin_test.go` from Task 1 fails until that line reads 16 — that is the guard working. Do not rebuild the extension and do not touch `ui/vscode/out/`: it is untracked build output, regenerated by whoever packages the extension.

Nothing else in the module pins a number: `ui/tui/rpcclient/client.go` sends `protocol.ProtocolVersion`, and the web UI echoes back whatever `core.health` reports.

- [ ] **Step 7: Document it**

In `docs/PROTOCOL.md` — which is written in Russian; match the surrounding prose — add `session.trajectory` beside the other session methods, and a `v16` entry at the top of the `История ProtocolVersion` list. State the three things a client needs: the params, the result shape, and that `recorded: false` means the session predates the log rather than being empty. Also note that the on-disk session schema stays at v4 and the log is a sidecar file.

- [ ] **Step 8: Run everything and commit**

```bash
go vet ./... && go test ./...
node ui/web/scripts/check-web.mjs
```

Expected: green, including `TestVSCodeExtensionPinsCurrentProtocolVersion` now asserting 16.

```bash
git add protocol/version.go internal/core docs/PROTOCOL.md ui/vscode/src/coreSession.ts
git commit -m "feat(core): session.trajectory, and ProtocolVersion 16

Reads a session's append-only event log back as {recorded, events[]}.
recorded:false means the session predates the log, which is not the same
as a log with no events — the view has to say which, so the wire says
which.

The version moves in one commit across everything that pins it: the Go
constant and the VS Code extension's copy. The on-disk session schema is
untouched and stays at v4; the log is a sidecar file."
```

---

## Self-Review

**1. Spec coverage.**

| Spec requirement | Task |
|---|---|
| Append-only per-session log, sidecar at `<id>.events.jsonl` | 2 |
| Monotonic contiguous seq, epoch-ms timestamp, type, payload, `source` | 2 |
| `source` present but unpopulated, for a later plugin layer | 2 (`SourceCore`) |
| Step boundaries and timing recorded at the source, not the observer | 2 (`TimeMS` stamped in `Append`) + 3 (recorded in the core) |
| Per-step token usage from the provider's response | 3 — `step_usage` already carries it; recording the notification stream captures it |
| Session snapshot schema stays at v4 | 2, and stated in Global Constraints |
| `sessionfile.Delete` removes the sidecar | 2 |
| `ProtocolVersion` 15 → 16, lockstep across clients | 4, guarded by the test added in 1 |
| A session with no log reports it rather than showing empty | 2 (`recorded`) + 4 (on the wire) |
| Nothing synthesises a log; no estimated numbers | Global Constraints; tested in 2 |
| `fork`/`rewind`/`search` keep passing unchanged | Global `go test ./...` in every task; they read the snapshot, which nothing here touches |
| Crash safety: torn trailing line | 2 |
| Round-trip, append-only, contiguous seq tests | 2 |
| The pre-existing loader data-loss bug | 1 |
| The view, the segmented control, VS Code parity, mid-turn replay | **C2b — not this plan** |

**2. Placeholder scan.** This section previously defended three placeholders as "the honest shape of follow-the-existing-pattern": `c.WorkspaceRoot()`/`c.logf` in Task 3 Step 5, and `newTestCore` in Task 4 Step 1. That defence was wrong, and reading the tree settled it. None of those three identifiers exists: the real names are `c.workspaceRoot`, the `fmt.Fprintf(os.Stderr, "core: session %s ...")` idiom, and `setupInitializedCore(t, root, &fixedLLM{})`. A placeholder inside a code block an implementer is told to use verbatim is not a pattern to follow — it is a compile error with an excuse attached, and it costs a review round to discover. All three now name the real thing. Task 3 Step 6 still leaves a test *body* to be written against the named harness, listing the three assertions required and forbidding a comment-only test; that one is a genuine "follow the worked example", because the example is named and reachable.

**3. Type consistency.** `trajectory.Event` fields (`Seq`, `TimeMS`, `Type`, `Source`, `Data`) are used identically in Tasks 2, 3 and 4. `Read` returns `([]Event, bool, error)` in its definition (Task 2), its consumer (Task 4), and the Interfaces blocks of both. `NewWriter`/`Append`/`Close` signatures match between Task 2's implementation and Task 3's use. `Path` is the only path helper; the Interfaces block explicitly says `SidecarPath` does not exist, because a second name for the same thing is how two spellings drift. The one deliberate duplication — `sessionfile.Delete` spelling the sidecar name by hand — is called out in the code comment and pinned by a test in both packages.
