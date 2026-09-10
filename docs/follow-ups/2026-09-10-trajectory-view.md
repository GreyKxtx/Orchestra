# Follow-ups: the Trajectory view (C2b)

Findings raised during C2b's reviews and its driven acceptance run that were judged
real and parked with a stated reason rather than fixed. None is a bug report awaiting
triage — each was seen, weighed and left deliberately, and the reasoning is the useful
part. What was fixed is not listed here; the ledger under `.superpowers/sdd/` has it.

C2b shipped in `feat/trajectory-view` after five task reviews, three task-level fix
rounds, two headless acceptance runs against a real core, a whole-branch review, and one
final fix round for what that review found. Two defects the first acceptance run found —
the pane never populated on first load, and every fresh session read "predates the
log" — had passed 41 adapter tests; both are fixed on the branch, and the lesson is
recorded in memory rather than here. The whole-branch review found three further,
narrower gaps that only the composed system could show — a launch-time race in the
core's answer, a cross-session live-event leak in the web host, and an expanded row
collapsing on rerender — closed in the final fix round and confirmed by a second
acceptance run (42 adapter tests, plus a live click-through of the expand fix).

Spec: `docs/superpowers/specs/2026-09-09-multi-project-window-design.md`
Plan: `docs/superpowers/plans/2026-09-10-trajectory-view.md`
C2a's follow-ups: `docs/follow-ups/2026-09-10-trajectory-event-log.md`

## Open acceptance — not a finding, the one thing left undone

**The Extension Development Host check (plan Task 4, Step 4) has not been run.** No
subagent can press F5 and click. The shared renderer and the web host were driven
headless against a real core and behaved (rows appear live, are replaced with recorded
ones carrying the core's timings, the empty and the not-recorded sentences are the
right ones). The VS Code host's three hooks are covered by `tsc`, a review that traced
every call site, and nothing else. The owner should open a workspace with a recorded
session, press F5 in `ui/vscode`, and read the six observations listed in the plan.
Until then the extension side is reviewed, not verified.

## From Task 1's review — the pure function

1. **`live` marks the whole turn when one trailing live event lands in it.** A recorded
   prefix with a live tail shows the turn row itself as live. Kept: the turn *is*
   ongoing when a live event arrives, and the item rows still distinguish recorded from
   live. Revisit if a user reads the turn's "live" as "these earlier rows are
   unconfirmed".

2. **`durationMs: 0` for a node touched once.** A `stage_done` with no `stage_start`,
   or a response that arrived as one coalesced delta, has `startMs === endMs` and
   renders "0ms" — the acceptance run showed exactly that on the `¶ response` row. An
   instantaneous event and a single-touch one are indistinguishable without a touch
   count. Honest fix: count touches and render blank below two. Small; parked because
   the row is truthful about what the log contains, only misleading about what it
   omits.

3. **`extractFunction` is duplicated between `check-webview.mjs` and
   `trajectory-test.mjs`.** Fifteen lines. `check-webview.mjs` runs its checks at top
   level, so importing it from the test would run them as a side effect; sharing means
   a third file. Not worth it at this size.

## From Task 2's review — the renderer

4. **Tool rows never show "· done".** By the brief's design: every completed tool would
   otherwise add the same suffix. The outcome is still on the row object for a future
   filter or icon.

5. **Three sequences no test covers.** A `trajectoryEvent` arriving before the first
   `trajectory` (works — `trajRecorded` flips to true); `recorded:false` with non-empty
   `events` (contradictory input; renders the rows); a burst of live events within one
   frame (coalesced by `trajRenderQueued`, by reading, not by assertion). The first is
   plausible in production and deserves a fixture when the harness next changes.

## From Task 3's reviews — the web host

6. **Two back-to-back turns racing their re-fetches.** The guard now drops an answer
   for a session the project has left, but two fetches for the *same* session can still
   land out of order, leaving a slightly stale snapshot until the next re-fetch. Bounded
   and self-correcting; a request counter per session would close it.

7. **The `catch` path of `refreshTrajectory` is untested.** Every fixture answers with
   success. The code reads correctly against the constraint (`recorded:true, events:[],
   error`), and the renderer's "Trajectory unavailable" path has its own test; the join
   between them does not.

8. **A WebSocket reconnect mid-turn leaves `sendTurn`'s captured `conn` on the dead
   socket.** `ensureConn` builds a replacement and `forgetProjectState` wipes the
   record, so the turn-end re-fetch goes nowhere. Intentional per the brief — the turn
   went out on that connection — and the next session start refreshes anyway.

9. **No test traps a background project's `onConnected` firing a fetch.** The source
   gate (`projectId === currentProjectId`) is correct by reading; the fixtures that open
   a background project only inspect its traffic after switching into it, so a spurious
   fetch would pass unnoticed. One negative assertion after `openBackground` would do.

10. **`projectState()` in the guard auto-vivifies an orphan map entry after a project is
    forgotten.** Harmless: `renderProjects` iterates the server's list via
    `peekProjectState`. `peekProjectState` in the guard would avoid the orphan.

## From Task 4's review — the VS Code host

11. **The turn-end re-fetch uses the session id at completion, not the one the turn ran
    on.** `openSession`/`newSession` do not check `sendInFlight`, so a mid-turn switch
    is reachable; the `finally` then fetches the new session's log. The guard keeps the
    wrong rows off the pane, and the old session self-heals when reopened. Mirrors the
    pre-existing `header` post two lines above, which has the same limitation.

12. **`forwardAgentEvent`'s `render` gate would drop the trajectory forward when
    `render === false`.** Every caller passes `true` today; the comment at the call
    site says why. If a future caller passes `false`, the pane misses live rows until
    the turn-end re-fetch.

13. **`workflow/stage_start|done` is dead wiring in the extension.** Those
    notifications come only from the standalone `workflow.run` RPC, which carries no
    session and is not teed into any log; nothing in `ui/vscode/src` calls it. The
    subscription was extended faithfully and forwards nothing. The renderer's stage
    rows are exercised by the recorded fixtures, not by this path.

## From Task 5 — the core's answer for a fresh session

14. **A session started by a detached core and not yet snapshotted reads
    `recorded:false` for a few seconds.** `GetOrLoad` cannot see it until its snapshot
    lands, so the pane says "predates the log" until then. Rare and transient; the
    right answer needs the turn lock's owner to say what it is doing.

## Tooling, found while driving the page

15. **`orchestra web --announce` ties the server's lifetime to stdin EOF.** Correct for
    its sidecar purpose, and a trap for a headless run whose stdin is closed at spawn:
    the server exits before the first request. Use `--port N --token T` for a
    standalone run; worth one sentence in the flag's help text.

16. **The extension's F5 check has no headless substitute.** `panel.ts` and
    `coreSession.ts` import `vscode` and have no test harness; the renderer they drive
    is covered through the web page instead. A `@vscode/test-electron` runner that opens
    the panel and reads the webview's posted messages would give the VS Code host what
    the adapter tests give the web host. Bigger than a follow-up; belongs with Part D's
    packaging work, where the extension is exercised anyway.

## From the final whole-branch review

Three Important findings from this pass went to a fix round (session-launch race in
`SessionTrajectory`, cross-session live-event leakage in the web host's forward, and
expanded-row state lost on rerender) — not listed here; the ledger has them. These
three Minors were parked instead.

17. **`SessionTrajectory`'s "empty" predicate is a narrower, duplicated copy of
    `sessionLooksRestoredLocked`.** It checks only `len(sess.History) == 0 &&
    len(sess.UIMessages()) == 0`; the pre-existing helper (`internal/core/session_rpc.go`)
    also checks todos and the plan path. Unreachable today — in this codebase neither
    can be non-empty while History and UIMessages both are — but it is a second, subtly
    incomplete copy of logic that already exists once. Prefer calling
    `sessionLooksRestoredLocked(sess)` (negated) directly.

18. **A failed turn-end re-fetch wipes the live rows the user already saw, replacing
    them with "Trajectory unavailable."** Both hosts' `catch` paths post `{recorded:
    true, events: [], error}`; the renderer's `replaceTrajectory` treats any `trajectory`
    message as a full replace, so a transient RPC hiccup right at turn end turns a
    correct, fully-populated live view into an empty error state instead of leaving the
    last-known-good rows up with a banner. The renderer would need to distinguish "no
    events because none happened" from "no events because this answer replaces nothing
    useful" — worth doing alongside item 6 below, which has the same shape.

19. **The web host's turn-end re-fetch has the same limitation already parked for the
    VS Code host as item 11: it reads the session id at completion time, not the one
    the turn ran on.** `ui/web/src/10-adapter-session.js`'s `sendTurn` `finally` reads
    `st.sessionId` (current, mutable); if the user switches sessions mid-turn — now
    reachable per the fix for the live-event leak above, which the forward guard closes
    but the fetch's own targeting does not — the old session's completed turn never gets
    its "replace live with recorded" refresh. Self-heals when the old session is
    reopened, the same property accepted for item 11.
