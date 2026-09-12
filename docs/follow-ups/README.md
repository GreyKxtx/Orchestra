# Open follow-ups

Everything still open from C1 (the multi-project rail), C2a (the trajectory
event log) and C2b (the Trajectory view), in one place.

These are not bug reports awaiting triage. Each was raised in a review, judged
real, and parked with a stated reason — and the reason is the useful part, so
it is kept. What was fixed at the time is not listed.

Re-triaged 2026-09-12 against the code as it stands. Forty-six findings were
recorded across the three original documents; two had been closed by later work
and are gone from this list rather than marked, so the list holds only what is
still true. Four items are escalated, two narrowed. The three source documents
are replaced by this one.

Specs: `docs/superpowers/specs/2026-09-09-multi-project-window-design.md`
Plans: `docs/superpowers/plans/2026-09-10-trajectory-{event-log,view}.md`

---

## 1. What only a person can close

Nothing in the build environment can press a key or see a toast. These four
were never exercised in a real window, and inventing the observations would
have been worse than leaving them open.

**1.1 The Extension Development Host check (C2b, plan Task 4 Step 4).** Open a
workspace with a recorded session, press F5 in `ui/vscode`, and read the six
observations listed in the plan. The shared renderer and the web host were
driven headless against a real core and behaved; the VS Code host's three hooks
are covered by `tsc`, a review that traced every call site, and nothing else.
Until this is run, the extension side is reviewed, not verified.

**1.2 The Windows notification (C1).** Give a background project work that needs
a permission answer and do not answer it: a toast should appear naming that
project. The underlying call was proven during C1's first task; only this
wiring is unverified.

**1.3 Two projects asking at once (C1).** Get two projects waiting on a
permission prompt simultaneously and switch between them. Each must show its
own prompt, and answering must affect only that project. This was a Critical
found by the whole-branch review — before the fix, the outgoing project's
prompt stayed on screen and "Allow always" wrote a rule into the other project.
It has a regression test; it has never been clicked.

**1.4 The keyboard path to close/forget (C1).** Tab to a chip, press `Shift+F10`
or the ContextMenu key, confirm the menu opens and both actions work. **No
automated coverage at all** — the harness's `closest()` stub returns null, so
rail key and click handlers cannot be driven. Verified by reading only.

Worth recording why this matters: two of the three defects C1's whole-branch
review found were visible from a single switch between two projects. That check
was skipped as "curl is close enough" during implementation and deferred again
afterwards, and both times it cost a full review-and-fix cycle.

---

## 2. Escalated in this triage

**2.1 The log still has no turn boundary event or duration field (was C2a 11).**
The recorded vocabulary has no `turn/start` or `turn/end`, and no step carries a
literal duration — a reader derives elapsed time from consecutive core-stamped
timestamps. The original finding said to decide this *before* C2b, because a
boundary event costs nothing to add now and **cannot be added retroactively to
sessions already recorded**. C2b has shipped and the event still does not exist.
The decision window closed; every session recorded from here on is missing it
permanently. This is the one item on the list that gets more expensive every day
it waits.

**2.2 `40-projects.js` now carries four jobs in 2035 lines (was C1 8).** It was
372 lines when the finding was raised — connection registry, HTTP client, rail
DOM, and input plus startup, behind comment banners. It is five and a half times
that now. The remedy has not changed and is still nearly free: the bundler
orders fragments numerically, so `41-projects-rail.js` is a move, not a rewrite.

**2.3 `renderProjects` rebuilds every chip, now from two places (was C1 1).**
`list.innerHTML = ""` destroys keyboard focus on a chip whenever *any* project's
status flips, and with a pulsing "working" status that is often. There were one
of these; there are now two (`40-projects.js:729` and `:1703`). The fix is keyed
reconciliation, not a one-liner.

**2.4 `SessionTrajectory`'s "empty" predicate — and its recommended fix is now
wrong (was C2b 17).** The finding said to call `sessionLooksRestoredLocked`
(negated) instead of keeping a second, narrower copy. Since then the predicate
gained a clause:

```go
empty := (len(sess.History) == 0 && len(sess.UIMessages()) == 0) || sess.IsBusy()
```

It is no longer a subset of that helper, and calling the helper directly would
now reintroduce the "a fresh session predates the log" bug that clause fixed.
The duplication is still real; the remedy has to be a shared predicate that
covers both, and whoever takes it must not follow the old instruction.

---

## 3. Narrowed in this triage

**3.1 Switching into a project mid-turn loses the live text (was C1 9).** The
answer named at the time was C2's append-only log — a whole subsystem. That
exists now, and the Trajectory pane replays it (`refreshTrajectory`). What is
left is narrower and named in the spec as the block that follows: the chat
transcript still repaints from `session.get`, which does not hold a partial
in-flight turn, so the tokens emitted while you were away are still missing
*there*. The spec's own words: the log is "authoritative for the trajectory and
for nothing else yet."

**3.2 `role="tablist"` over children that are not tabs (was C1 2).** Half of
this closed elsewhere: `view-switch` and `session-tabs` now carry real
`role="tab"` and `aria-selected`. `project-rail-list` (`index.src.html:65`) is
still a tablist over plain buttons. One place left, not a general problem.

---

## 4. The rail and the web host (C1)

**4.1 `forgetProject` does not move off the active project, but `closeProject`
does.** Forgetting the project you are looking at leaves `currentProjectId`
naming an entry no longer in `known`, with no chip for it. Asymmetric with its
sibling for no stated reason.

**4.2 Machine error codes are shown to the user.** `api()` sets `err.message`
from the response's `error` field, which is a code — `bad_request`,
`already_open`, `no_such_dir`, `open_failed` — so the user reads "could not open
C:\x: already_open". The server sends a `detail` field alongside it that nothing
reads.

**4.3 The harness's mocked `fetch` times around microtask ordering.** The
`await Promise.resolve()` deferral is correct in substance; accepting the
responder in `loadBundle({ fetchResponder })` would remove the coupling instead
of timing around it. Not racy today — this is about how easy it is to break.

**4.4 `turnStart`'s reset keys off delivery-time `currentProjectId`.**
`20-adapter-events.js` resets `turnTextByProject`/blocks for whichever project
is active when the message *drains*, and `toRenderer` is an async `postMessage`.
Sending in A and switching to B before it drains wipes B's accumulators. Narrow,
same class as the stale-guard findings that were fixed.

**4.5 `notifyAsking` fires on any `onServerRequest` where the status is already
`"asking"`,** not only on the idle→asking transition. Benign today — one core
request means one pending ask in practice — but a second request for a project
already mid-ask would overwrite `pendingAsk` and re-notify. The brief's own
reference code has the same shape.

**4.6 `ui/desktop/README.md` describes four chip states** while an internal
self-review table called the set "five". No inconsistency with the code:
`status` is a four-value enum and "active" is a separate boolean marker.
Wording only, inherited from the brief's verbatim text.

**4.7 `sendTurn`'s `connFor(projectId).sendCancellable(...)` has no null
guard,** where the deleted `wsSendCancellable` would have rejected gracefully.
Safe by construction today — `projectId` is read from `currentProjectId`
synchronously with no yield point before `connFor` — but a less defensive shape
than what it replaced.

**4.8 The keyboard-opened rail menu does not move focus into itself.** A
keyboard user must Tab away from the chip to reach "Close project" / "Remove
from list". Mirrors the mouse path exactly. The natural follow-up to 3.2.

**4.9 `activeConn()` in `00-web-prelude.js` has zero callers** anywhere in
`ui/web`; only `setActiveConn` is used. Pre-existing dead code — still dead as
of this triage. The fix round's report justified keeping it by pointing at the
`active` variable it wraps, which `wsNotify` does read directly; that is the
variable, not the accessor.

**4.10 `closeProject` / `forgetProject` never explicitly hide the overlay.** If
the only remaining project is closed while its own permission ask is on screen,
the overlay stays up with dead buttons until the next switch. Safe by
construction — `projectState()` lazily rebuilds an empty record for the deleted
id, so a stray reply is dropped by the `!st.pendingAsk` guard — and identical in
the pre-fix code.

---

## 5. The event log (C2a)

**5.1 The pin test's regexp takes the first textual `PROTOCOL_VERSION = <n>`.**
`internal/cli/protocol_pin_test.go` greps `coreSession.ts` from Go, so a
commented-out or duplicated earlier assignment would be read instead of the live
one. It catches the only failure that has actually happened — a bare constant
left at an old number, which is how the extension came to sit at 14 while the
core was at 15 — and parsing TypeScript from Go is out of proportion.

**5.2 `internal/sessionfile/migrate.go`'s error says "parse v2"** when the
branch it guards covers v2 through v5 and beyond. Pre-existing, but it will
mislead whoever next reads a migration failure.

**5.3 `lastSeq` rescans the whole sidecar on every `NewWriter`.** Once per
writer, not per append, so a turn pays it once. The cost grows with the
session's length and nothing trims the log. The original note said to re-rate
this "when C2b makes long sessions with long logs the normal case" — C2b has
shipped, so that trigger has fired, though no long-session measurement has been
taken yet.

**5.4 A corrupt line mid-log is dropped as silently as an expected torn tail.**
`Read` skipping any unparseable line is deliberate and should stay: a log
outlives the code that reads it, and one bad line must not deny a reader the
other thousand. What is parked is the *observability* half — no count, no
signal. Closing it means changing `Read`'s signature, which `session.trajectory`
depends on.

**5.5 `TestAppend_AFailedWriteDoesNotReissueItsSequenceNumber` is a
characterization test, not a regression test.** It passes against the pre-fix
code. Kept because it pins the invariant and is the only coverage of the
`tornWrite` flag. A genuine partial write cannot be manufactured without making
`Writer.f` an injectable `io.Writer`.

**5.6 The two `prepareAgentLaunch` error paths that could leak the writer are
unreachable today** (`:233`, `:354`). The `defer` keyed on the named error
return stays as defence for paths the function grows later. Unreachability was
verified three ways: `validateAgents` rejects the only config that reaches
`:233`; `AgentsUpsert` enforces the same rule at runtime; no RPC deletes a
provider. A future provider-delete API would make the path live.

**5.7 The leak test builds its situation by hand.** Because of 5.6 it cannot
provoke the failure through configuration, so it constructs the core with no
injected LLM client and appends to `c.cfg.Agents` in memory, bypassing the
validation every real entry point enforces. Precedent exists in the package.

**5.8 `ui/desktop/src-tauri/src/boot.rs` parses and stores a `protocol_version`
and never compares it to anything.** Harmless today because nothing reads it —
and on this list because **it is the same shape as the bug the pin test exists
to fix**. A stored-but-unchecked version is a pin waiting to be wired up at
whatever number it happens to hold. When packaging lands, either compare it or
delete it.

**5.9 `SessionClose` does not wait for a cancelled turn to release the log.**
The delete now happens before in-memory state is dropped and the error is
returned truthfully, so a failure destroys nothing. It does not make the delete
succeed while a turn is live. Acquiring `c.runMu` after `sess.Cancel()` would
work but puts new blocking semantics into an RPC method, and a deadlock against
a caller already holding `runMu` cannot be ruled out without analysis. Meanwhile
on Windows, deleting a session during a live turn fails with a retryable error —
a truthful failure in place of the false success it replaced.

**5.10 The context estimate is emitted twice in one step after compaction.**
`agent_run.go:261` fires `emitPromptContextEstimate` every step and `:244` fires
it again after a compaction. Duplicate noise rather than a wrong number, since
the estimate has its own event kind. Deduplicating means deciding which of the
two is authoritative.

**5.11 `pickExistingBinary` chooses the core binary by modification time, not by
the priority order its own doc comment describes.** The comment says "candidate
paths in priority order" and puts the bundled binary first; the code keeps
whichever has the greatest `mtimeMs`. Today recency works in our favour, but the
bundled binary wins the moment the root one is absent or older — and then a
current extension spawns a stale core and fails the handshake. The protocol has
since moved to 18, so the gap a stale bundled binary would open is wider than
when this was written. The comment and the code should agree.

---

## 6. The Trajectory view (C2b)

**6.1 `live` marks the whole turn when one trailing live event lands in it.** A
recorded prefix with a live tail shows the turn row itself as live. Kept: the
turn *is* ongoing when a live event arrives, and the item rows still distinguish
recorded from live. Revisit if a user reads the turn's "live" as "these earlier
rows are unconfirmed".

**6.2 `durationMs: 0` for a node touched once.** A `stage_done` with no
`stage_start`, or a response that arrived as one coalesced delta, renders "0ms".
An instantaneous event and a single-touch one are indistinguishable without a
touch count. Honest fix: count touches, render blank below two.

**6.3 `extractFunction` is duplicated between `check-webview.mjs` and
`trajectory-test.mjs`.** Fifteen lines. `check-webview.mjs` runs its checks at
top level, so importing it would run them as a side effect; sharing means a
third file. Not worth it at this size.

**6.4 Tool rows never show "· done".** By design: every completed tool would
otherwise add the same suffix. The outcome is still on the row object for a
future filter or icon.

**6.5 Three sequences no test covers.** A `trajectoryEvent` before the first
`trajectory` (works); `recorded:false` with non-empty `events` (contradictory
input; renders the rows); a burst of live events in one frame (coalesced by
reading, not by assertion). The first is plausible in production and deserves a
fixture when the harness next changes.

**6.6 Two back-to-back turns racing their re-fetches.** The guard drops an
answer for a session the project has left, but two fetches for the *same*
session can still land out of order, leaving a slightly stale snapshot until the
next re-fetch. A request counter per session would close it. Same shape as 6.15.

**6.7 The `catch` path of `refreshTrajectory` is untested.** Every fixture
answers with success. The code reads correctly against the constraint and the
renderer's "Trajectory unavailable" path has its own test; the join between them
does not.

**6.8 A WebSocket reconnect mid-turn leaves `sendTurn`'s captured `conn` on the
dead socket.** `ensureConn` builds a replacement and `forgetProjectState` wipes
the record, so the turn-end re-fetch goes nowhere. Intentional — the turn went
out on that connection — and the next session start refreshes anyway.

**6.9 No test traps a background project's `onConnected` firing a fetch.** The
source gate (`projectId === currentProjectId`) is correct by reading; fixtures
that open a background project only inspect its traffic after switching in, so a
spurious fetch would pass unnoticed. One negative assertion would do.

**6.10 `projectState()` in the guard auto-vivifies an orphan map entry after a
project is forgotten.** Harmless: `renderProjects` iterates the server's list
via `peekProjectState`, which in the guard would avoid the orphan.

**6.11 The VS Code turn-end re-fetch uses the session id at completion, not the
one the turn ran on.** `openSession`/`newSession` do not check `sendInFlight`,
so a mid-turn switch is reachable; the `finally` then fetches the new session's
log. The guard keeps the wrong rows off the pane and the old session self-heals
when reopened. Mirrors the pre-existing `header` post two lines above.

**6.12 `forwardAgentEvent`'s `render` gate would drop the trajectory forward
when `render === false`.** Every caller passes `true` today. If a future caller
passes `false`, the pane misses live rows until the turn-end re-fetch.

**6.13 `workflow/stage_start|done` is dead wiring in the extension.** Those
notifications come only from the standalone `workflow.run` RPC, which carries no
session and is not teed into any log; nothing in `ui/vscode/src` calls it. The
renderer's stage rows are exercised by recorded fixtures, not by this path.

**6.14 `orchestra web --announce` ties the server's lifetime to stdin EOF.**
Correct for its sidecar purpose, and a trap for a headless run whose stdin is
closed at spawn: the server exits before the first request. Use `--port N
--token T` for a standalone run; worth one sentence in the flag's help text.

**6.15 A failed turn-end re-fetch wipes the live rows the user already saw,**
replacing them with "Trajectory unavailable." Both hosts' `catch` paths post
`{recorded: true, events: [], error}`, and `replaceTrajectory` treats any
`trajectory` message as a full replace — so a transient RPC hiccup at turn end
turns a correct, fully-populated live view into an empty error state. The
renderer needs to distinguish "no events because none happened" from "no events
because this answer replaces nothing useful". Worth doing alongside 6.6.

**6.16 The web host's turn-end re-fetch has the same limitation as 6.11:** it
reads `st.sessionId` at completion time, not the one the turn ran on. If the
user switches sessions mid-turn the old session's completed turn never gets its
"replace live with recorded" refresh. Self-heals when the old session is
reopened.

**6.17 The extension's F5 check has no headless substitute.** `panel.ts` and
`coreSession.ts` import `vscode` and have no test harness; the renderer they
drive is covered through the web page instead. A `@vscode/test-electron` runner
that opens the panel and reads the webview's posted messages would give the VS
Code host what the adapter tests give the web host. Bigger than a follow-up;
belongs with the packaging work, where the extension is exercised anyway. This
is what makes 1.1 a manual step rather than a test.
