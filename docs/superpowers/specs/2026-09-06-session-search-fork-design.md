# Session search and fork (§1.4 #1–2)

Status: implemented. **Amended 2026-09-06** after a whole-branch review — see
"Amendment: the UI↔history mapping" below. The original design rested on a
premise about `History` that was simply false, and fork did not work on a
single real session because of it. The amendment is the binding design; where
the sections above it still describe the counting approach, they are marked.
Plan reference: `docs/parity-plan-2026-09.md` §1.4 #1 (поиск по сессиям) and #2 (fork/branch от сообщения).

## Problem

Sessions accumulate — dozens per project under `.orchestra/sessions/*.json` — and two
things are missing once there are more than a handful:

1. **Finding one.** The only way to locate a past session is `orchestra session list`
   (a table of ids and titles) or the TUI picker's fuzzy filter, which matches
   `Meta.Title` only (`ui/tui/view/dialog_sessions.go:120-134`). `Title` is itself
   just the first user message truncated to 60 runes
   (`internal/sessionfile/migrate.go:258-279`), so nothing that happened after the
   opening prompt is searchable at all.
2. **Branching from one.** `session.rewind` can return to an earlier checkpoint, but
   it truncates in place and overwrites the same file
   (`internal/core/session_rewind.go:60-70`) — the discarded messages are gone, with
   no backup and no undo. There is no way to try a different path from step 7 while
   keeping what the original run produced.

## What already exists (verified, not assumed)

- `Snapshot` (`internal/sessionfile/snapshot.go:19-36`) holds `History []llm.Message`
  and `UIMessages []UIMessage` — two parallel, position-indexed arrays. **No message
  carries a stable id**; the only identity is array position. `session.rewind` copes
  by counting user messages rather than indexing, and anything built here inherits
  that constraint.
  > **WRONG — superseded.** The two arrays are not parallel at all, and counting
  > user messages does not map one onto the other. See the amendment.
- `Snapshot` has **no lineage field** — no `parent_id`, no `forked_from`.
- `sessionfile.Import` (`internal/sessionfile/export.go:99-125`) already writes "the
  same snapshot under a different id", which is half of a fork.
- `truncateHistoryForUIPrefix` (`internal/core/session_rewind.go:81-107`) already maps
  a UI prefix onto a history cut, by counting user messages.
- `sessionfile.ListMeta` (`internal/sessionfile/store.go:74-110`) already reads **and
  fully parses every session file** on every call, and the TUI picker calls it each
  time `/sessions` opens.
- `NewSessionsDialog(metas []sessionstore.SessionMeta)` (`ui/tui/view/dialog_sessions.go:25-27`)
  takes an already-loaded list, so the picker can be seeded with a filtered set
  without touching the view.
- `LoadFromDisk` hard-rejects a snapshot whose `Version` differs from the binary's
  (`internal/core/session/persist.go:101-103`), and `LoadOrCreate`
  (`persist.go:220`) loads from disk whenever the id is not already in the Manager's
  cache.
- `orchestra search` (the code-search command) defaults to **case-sensitive** and
  takes `-i/--insensitive` (`internal/cli/search.go:27`).

## Non-goals

- **Not** changing `session.rewind`'s behaviour. Rewind stays destructive and stays
  the fast "cut the junk out of this session" operation; fork is a separate,
  non-destructive operation beside it. `/rewind`, its dialog, and the VS Code call
  site are untouched.
- **Not** a search index or a database. `ListMeta` already parses every file on
  every picker open, so content search adds no new class of I/O — only more work on
  bytes already being read. An index would be the SQLite decision §1.4 #4 already
  closed as "не делать".
- **Not** a VS Code UI. The two RPC methods are added so the extension *can* use
  them, but building webview surfaces for search and fork belongs to §1.9.
- **Not** a message-level results browser in the TUI (jump straight to the matching
  message). That needs a new dialog kind plus scroll-to-index, and the plan's own
  wording for this item is "`/sessions` с фильтром" — a filter, not a browser.
  Message-level hits with snippets are delivered by the CLI.
- **Not** regex search. Plain substring matching only.

## Architecture

Logic lives as pure functions over `Snapshot` in `internal/sessionfile`; every
surface is a thin wrapper. This follows what the code already does — the CLI reads
session files directly through `sessionfile`/`sessionstore` and does not go through
JSON-RPC — and it means one implementation serves CLI, TUI, and RPC alike.

| File | Responsibility |
|---|---|
| `internal/sessionfile/history.go` (new) | `IndexOfNthUserMessage` — the shared history-cut helper |
| `internal/sessionfile/search.go` (new) | `Search`, `Hit`, `SearchOptions` |
| `internal/sessionfile/fork.go` (new) | `ForkSnapshot` — pure transform, no I/O |
| `internal/core/session_rewind.go` (modify) | switches to the shared helper; behaviour unchanged |
| `internal/core/session_fork.go` (new) | `session.fork` RPC over a possibly-live session |
| `internal/core/session_search.go` (new) | `session.search` RPC |
| `internal/core/rpc_handler.go` (modify) | two new `case` arms |
| `internal/cli/session.go` (modify) | `search` and `fork` subcommands |
| `ui/tui/...` (modify) | `/fork` command; `/sessions <query>` content filter |
| `docs/PROTOCOL.md` (modify) | two method sections + protocol version bump |

### The shared history helper

`truncateHistoryForUIPrefix` keeps `hist[:i+1]` — history **through and including**
the Nth user message. Fork needs `hist[:i]` — everything **before** it, so the
assistant's reply to the previous step survives. One character apart, opposite
meaning, so neither should be expressed in terms of the other. Both are expressed in
terms of a single locator:

```go
// IndexOfNthUserMessage returns the position of the nth (1-based) user message in
// hist, or -1 when there are fewer than n user messages. Callers decide whether to
// cut inclusively (rewind) or exclusively (fork).
func IndexOfNthUserMessage(hist []llm.Message, n int) int
```

`truncateHistoryForUIPrefix` keeps its exact current contract, including its
fallback: when the Nth user message is not found (history was rewritten by
`/compact`), it keeps the **full** history rather than truncating too far. That
fallback is conservative for rewind and is not being changed here.

### Search

```go
// Hit is one matching message inside one session.
type Hit struct {
    SessionID string    `json:"session_id"`
    Title     string    `json:"title"`
    UpdatedAt time.Time `json:"updated_at"`
    Index     int       `json:"index"` // ui_messages index — the same index fork and rewind take
    Role      string    `json:"role"`
    Snippet   string    `json:"snippet"`
}

type SearchOptions struct {
    Query       string
    Insensitive bool // mirrors `orchestra search -i`; default is case-sensitive
    IncludeAll  bool // also search Reasoning and ToolBlocks, not just Text
    Limit       int  // cap on total hits; 0 means no cap
}

func Search(workspaceRoot string, opts SearchOptions) ([]Hit, error)
```

- **Fields searched.** By default only `UIMessage.Text` — the user/assistant prose,
  i.e. "what we talked about". `IncludeAll` adds `Reasoning` and `ToolBlocks`
  (arguments and output). Tool output is large and noisy; searching it by default
  would bury the prose hits it is meant to sit beside.
- **One hit per message.** The first match only. A single tool-output message
  containing the query forty times must not produce forty rows.
- **Snippet.** The matched line, whitespace-collapsed, trimmed to 120 runes centred
  on the match, with `…` where either end was cut.
- **Ordering.** Sessions by `UpdatedAt` descending (matching `ListMeta`), and within
  a session by message index ascending. `Limit` truncates after ordering, so the cap
  keeps the most recent sessions rather than an arbitrary set.
- **Unreadable files are skipped, not fatal.** One corrupt session file must not
  make search fail for the other fifty-one. `ListMeta` already behaves this way.
- **Titles are not searched separately.** A session's title is derived from its first
  user message, so any title match is already a content match on that message. The
  picker's existing fuzzy-over-titles filter is unrelated and stays as it is.

### Fork

```go
// ForkSnapshot returns a new snapshot containing everything strictly before
// uiIndex. snap is not modified.
func ForkSnapshot(snap *Snapshot, uiIndex int, newID string) (*Snapshot, error)
```

Semantics, chosen so that "try step 7 differently" reads naturally:

- `uiIndex` must point at a **user** message — the same checkpoint rule
  `session.rewind` enforces (`internal/core/session_rewind.go:52-58`).
- The branch keeps `UIMessages[:uiIndex]` — **excluding** the message at `uiIndex`.
  The branch therefore ends with the assistant's answer to the previous step, and
  the next thing written into it is a fresh prompt. Keeping the old prompt would
  leave two user messages in a row once a new one is sent.
- History is cut with `IndexOfNthUserMessage(hist, k+1)` where `k` is the number of
  user messages in `UIMessages[:uiIndex]`, keeping `hist[:i]`.
  > **WRONG — superseded.** History is cut at `TurnStarts[k]`. See the amendment.
- `PendingOps` and `Todos` are cleared, matching what rewind does
  (`session_rewind.go:65-66`) — they describe work from the abandoned path.
- `ID` is a fresh `NewID()`; `CreatedAt`/`UpdatedAt` are stamped by `Save`.
- `Title` becomes the parent's title plus `" (fork)"`. Titles are derived from the
  first user message, which the branch shares verbatim with its parent, so without
  this the picker shows two identical rows.

**Failures, chosen so a wrong branch is never produced silently:**

- `uiIndex == 0` → error. The branch would contain nothing.
- `uiIndex` out of range, or not a user message → error naming the actual role.
- No recorded turn boundary for the requested turn → error saying the session has
  none: it predates the feature, or `/compact` rewrote its history. Rewind's
  fallback in this situation is to keep the entire history; for a fork that same
  fallback would produce a "branch" that still contains everything it was supposed
  to branch away from. A refusal the user can act on beats a branch that looks
  right and is not.
  > **Amended.** The original wording named compaction as *the* cause, which was
  > exactly backwards — see the amendment.

### Lineage

Two additive fields on `Snapshot`:

```go
ParentID        string `json:"parent_id,omitempty"`
ForkedFromIndex int    `json:"forked_from_index,omitempty"`
```

**The schema version stays at 4.** `LoadFromDisk` rejects any snapshot whose
`Version` is not exactly the binary's own (`internal/core/session/persist.go:101-103`),
so bumping to 5 would make every file written by the new binary unreadable by an
older one. An `omitempty` field is ignored by an older binary's `json.Unmarshal` and
absent-means-zero in the new one, which is exactly the compatibility both directions
need.

### Live sessions

The file on disk lags a live session by up to the 5-second mid-turn snapshot
interval, and the Manager holds the authoritative in-memory copy. `session.fork`
therefore:

1. `GetOrLoad` the parent, take its lock, refuse if `IsBusy()` — same guard rewind
   uses.
2. Read `UIMessages()` and `CopyHistory()` from memory, not from disk.
3. Build the branch with `ForkSnapshot` and `Save` it.
4. Leave the parent completely untouched — no mutation, no re-snapshot.
5. **Not** register the branch in the Manager. The client's following
   `session.start` picks it up from disk through `LoadOrCreate`
   (`persist.go:220`), which avoids two owners for one id.

The CLI's `session fork` operates on the file instead, and its help text says so —
forking a session that is open elsewhere may miss the last few seconds.

## Surfaces

**CLI** (`internal/cli/session.go`, following the existing subcommand pattern):

```
orchestra session search <query> [-i|--insensitive] [--all] [--limit N]
orchestra session fork <session-id> --at <ui-index>
```

Search output is grep-shaped, one line per hit, so the index can be read straight
off it and handed to `fork`:

```
20260905T101500-3a2f  #12  user       …the bearer token never reaches the wire…
20260905T101500-3a2f  #18  assistant  …authTransport sets Authorization on every…
```

Sessions are separated by a header line carrying the title and update time. A query
matching nothing exits 0 with a short "no matches" line, not an error — the same
posture `orchestra session list` takes for an empty project.

`session fork` prints the new session's id on its own line, so it can be piped
straight into other commands.

**RPC** (`internal/core/rpc_handler.go`, one `case` each, following `session.rewind`
at `rpc_handler.go:185-192`):

- `session.search` — params `{query, insensitive, include_all, limit}`, result
  `{hits: []Hit}`.
- `session.fork` — params `{session_id, ui_message_index}`, result
  `{session_id, parent_id, ui_messages, history_messages}` where `session_id` is the
  **new** branch.

Both get a section in `docs/PROTOCOL.md` and a protocol version bump, as
`session.rewind` did for v7.

**TUI:**

- `/fork` opens the existing rewind checkpoint dialog (it already lists user
  messages), forks at the chosen point, then switches into the branch through the
  existing `loadSession(id)` path used when picking from `/sessions`.
- `/sessions <query>` runs a content search and opens the picker seeded with only
  the matching sessions. `NewSessionsDialog` already accepts a prepared list, so the
  dialog itself does not change; bare `/sessions` keeps today's behaviour exactly.

## Testing

Real files in `t.TempDir()`, no filesystem mocks — the approach every existing test
in this subsystem uses.

- `internal/sessionfile`: `IndexOfNthUserMessage` (found, not found, n=0);
  `Search` (match/no match, case sensitivity, `IncludeAll` reaching tool blocks,
  one-hit-per-message, ordering, `Limit`, a corrupt file being skipped rather than
  failing the run); `ForkSnapshot` (exclusive boundary keeps the previous
  assistant reply, lineage fields set, parent value untouched, index 0 refused,
  non-user index refused, compacted history refused).
- `internal/core`: `session.fork` through `setupSessionV2Core` — parent file
  byte-identical afterwards, branch present on disk with the expected lengths,
  `IsBusy` refused, and the branch loadable by a following `session.start`.
  `session.search` through `h.Handle`, as `rpc_handler_test.go` does for other
  methods.
- `internal/core/session_rewind_test.go`: keeps passing unchanged — that is the
  regression proof that moving to the shared helper did not alter rewind.
- `internal/cli`: both commands, following `TestSessionExportImportCLI`'s technique
  (temp dir, `.orchestra.yml`, package-level flag vars, `runX(nil, args)`).
- `ui/tui`: `/sessions <query>` seeds the dialog with the filtered set; `/fork`
  switches the active session id.

## Amendment: the UI↔history mapping (2026-09-06)

### The premise was false

Everything above assumed `Snapshot.UIMessages` and `Snapshot.History` could be
mapped onto each other by counting user messages: the Nth user message in the
UI corresponds to the Nth `role=user` entry in history. **That is not true, and
it was never true.**

`internal/agent/agent_step.go:52-68` builds a **fresh per-request** slice —
`system + user + history` — for every LLM call. The user's prompt is a
parameter of the request, not a member of the persisted array. It is never
appended to `History`, which holds assistant steps and tool results only. The
comment at `agent_step.go:52` says exactly this; the design read the two arrays
as parallel and did not check.

The consequences, all of them shipped:

- `IndexOfNthUserMessage(history, k+1)` returned `-1` on every ordinary
  session, so `ForkSnapshot` **refused all real data**. Fork did not work at
  all.
- The refusal blamed compaction, which is precisely inverted. A compacted
  session is the *only* kind that has a `role=user` entry in history at the
  front — `internal/agent/compact.go:111` prepends the summary as one.
- The agent also injects **synthetic** `role=user` messages mid-run: LSP
  diagnostic hints, retry nudges, image carriers. Where the count did resolve,
  it resolved against one of those, cut history mid-turn, and produced a
  **silently corrupt branch**.

This survived ten commits because every fixture — in `internal/sessionfile`,
`internal/core` and `internal/cli` alike — hand-built an alternating
user/assistant history, a shape the product never writes. The fixtures agreed
with the spec; neither agreed with the code.

### The fix: record the boundary instead of inferring it

`Snapshot` gains one additive field:

```go
TurnStarts []int `json:"turn_starts,omitempty"`
```

`TurnStarts[k]` is the index into `History` at which the **(k+1)-th user
turn's agent output begins**. For a session recorded from its first turn,
`TurnStarts[0] == 0`.

**The schema version stays at 4**, for the same reason `ParentID` did:
`LoadFromDisk` (`internal/core/session/persist.go:101-103`) hard-rejects any
version but the binary's own, and an `omitempty` field an older binary does
not know is simply ignored by `json.Unmarshal`.

Fork cuts at `TurnStarts[k]` where `k = CountUserMessages(prefix)`. The
locator is `sessionfile.TurnStartAt`, the one place fork and rewind agree on
where a turn starts. The branch keeps `TurnStarts[:k]`, so a branch is itself
forkable.

### Invariants

The field is only as good as the places that maintain it. Each of these is a
place it can go stale, and each has a test:

1. **Recorded at turn start.** `SessionMessage` appends the current
   `len(history)` when a user turn begins. Not at turn end: the `OnStepHistory`
   mid-turn snapshot replaces history with **partial-turn** content, so a
   turn-end computation would be wrong for every turn during which one fired.
2. **Mid-turn snapshots do not append.** `persistMidTurnHistory` writes history
   and nothing else. Appending there would invent a turn per five-second tick.
3. **Compaction clears them.** `SessionCompact` rewrites history wholesale, so
   every recorded index points into an array that no longer exists.
   `applyCompactedHistory` clears the field, and fork then refuses honestly.
4. **Rewind truncates them**, alongside ui and history.
5. **A turn that ends without a `*agent.Result` clears them.** The core
   persists a failed turn's history on purpose (`session_rpc.go`), and every
   error return in the agent loop yields `(history, nil, err)` — so a turn
   that compacted at step N and was cancelled at step N+1 would install a
   rewritten array under the old boundaries. `res == nil` means we cannot
   know, and clearing is the honest answer; the cost is that a cancelled turn
   ends fork for that session, exactly as `/compact` does.
6. **`RefreshFromDiskIfNewer` copies them.** It copies field by field, and
   `SessionMessage` refreshes immediately before a turn — a field it forgets
   goes stale exactly when the history beside it changes.

The same round-trip gap was silently erasing `ParentID` and `ForkedFromIndex`:
`toSnapshot` never wrote them, so a branch lost its lineage on its first save
after `session.start`. Both are now carried too.

### Amendment to the non-goals: rewind uses the boundary

The original non-goals said rewind's behaviour would not change. It does now,
and deliberately.

`truncateHistoryForUIPrefix` cuts at the recorded boundary when the session has
one, and falls back to today's exact behaviour — including its
keep-the-full-history fallback — when it does not.

The reason it changed: a task on this branch fixed a bug where the rewind
dialog discarded the command that reaches core, so rewind's persistence path is
newly live. With history left uncut, `/rewind` would visibly discard messages
the model still remembers. With real boundaries, rewind and fork make the
**identical** history cut and differ only in the UI prefix — rewind keeps the
prompt so the user can edit and resend it, fork drops it — which is coherent
precisely because the prompt itself never lives in history.

Rewind keeps its conservative fallback because it is destructive. Fork refuses
instead, because for a fork the same fallback would produce a branch containing
everything it was meant to branch away from.

### Testing

The lesson of this defect is that fixtures agreeing with the spec prove
nothing. Fork and rewind are now tested against a history shaped the way the
product actually writes it — assistant and tool messages only, no ordinary user
turns, plus one synthetic `role=user` hint sitting mid-turn — at both the
`sessionfile` and `core` levels. The pre-existing alternating fixtures were
reshaped to match reality rather than left as decoration.

## Known inaccuracy in the parity plan

§1.4's "Есть" line credits an "авто-заголовок (`title.txt`)". There is no
LLM-generated title: `agent.ModeTitle` has no caller anywhere, and
`internal/prompt/files/title.txt` is dead code. Titles come from
`TitleFromUIMessages` (`internal/sessionfile/migrate.go:258-279`), a truncation of
the first user message. The plan line should be corrected when §1.4 is updated.
