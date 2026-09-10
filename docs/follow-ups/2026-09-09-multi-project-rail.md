# Follow-ups: the multi-project rail (C1)

Fifteen findings raised during C1's reviews, each judged real and each parked
with a stated reason rather than fixed. None is a bug report awaiting triage —
they were all seen, weighed and left deliberately, and the reasoning is kept
because the reasoning is the useful part.

C1 shipped in `feat/multi-project-window` (18 commits) after nine task reviews,
two fix rounds and a whole-branch review. What was fixed is not listed here.

**Item 9 is the one that matters most, and it is not for this list to fix.** It
is a design consequence, and the owner has chosen to answer it in C2 by
persisting an append-only event log in the core. It is recorded here because it
explains a user-visible symptom: switching into a project mid-turn loses the
live reasoning and tool indicators, because there is no log to replay.

Spec: `docs/superpowers/specs/2026-09-09-multi-project-window-design.md`

---

## Parked from the Tasks 7+8 review (all Minor as reported)

1. **`renderProjects` rebuilds every chip on any status change.**
   `40-projects.js` does `list.innerHTML = ""` and recreates all chips, so
   keyboard focus on a chip is destroyed whenever *any* project's status flips.
   With a pulsing "working" status this can happen often. Parked because the fix
   is keyed reconciliation, not a one-liner. Severity may deserve re-rating: a
   keyboard user loses focus repeatedly during normal use.

2. **`role="tablist"` over children that are not tabs.** `index.src.html` gives
   the list `role="tablist"` but the chips are plain buttons with no
   `role="tab"` and no `aria-selected`, which is an invalid ARIA
   parent/child pairing. There is also no keyboard path to the context menu at
   all — it is `contextmenu`-only, so close and forget are mouse-only actions.

3. **`forgetProject` does not move off the active project, but `closeProject`
   does.** Forgetting the project you are looking at leaves `currentProjectId`
   naming an entry no longer in `known`, with no chip for it. Asymmetric with
   its sibling for no stated reason.

4. **Machine error codes are shown to the user.** `api()` sets `err.message`
   from the response's `error` field, which is a code — the server returns
   `bad_request`, `already_open`, `no_such_dir`, `open_failed` — so the user
   sees "could not open C:\x: already_open". The server sends a `detail` field
   alongside it that nothing reads.

5. **Orphaned `wsReply` in the prelude.** `00-web-prelude.js` still exports
   `wsReply`, which lost its last caller when `30-adapter-asks.js` moved to
   `connFor(...).reply`. Dead code in the file that otherwise defines the
   connection contract.

6. **The harness's mocked `fetch` times around microtask ordering.** The
   `await Promise.resolve()` deferral is correct in substance, but accepting the
   responder in `loadBundle({ fetchResponder })` would remove the coupling
   instead of timing around it. The re-review confirmed the current arrangement
   is not racy; this is about how easy it is to break later.

7. **`turnStart`'s reset keys off delivery-time `currentProjectId`.**
   `20-adapter-events.js` resets `turnTextByProject`/blocks for whichever
   project is active when the message *drains*, and `toRenderer` is an async
   `postMessage`. Sending in A and switching to B before it drains wipes B's
   accumulators. Very narrow, and the same class as the stale-guard findings
   that were fixed.

8. **`40-projects.js` carries four jobs in 372 lines** — connection registry,
   HTTP client, rail DOM, and input plus startup — behind comment banners. The
   bundler orders fragments numerically, so `41-projects-rail.js` would be a
   free split.

## Parked from the Tasks 7+8 fix-round re-review

9. **A background project's streamed text is never accumulated, so switching
   back mid-turn shows a gap.** `noteProjectEvent` only reaches
   `handleNotification` for the active project, so `turnTextByProject` never
   grows for a background one. Switching into a project mid-turn shows the
   transcript up to the last *completed* message plus whatever streams from
   that moment on; the tokens emitted while you were away are missing, because
   a partial in-flight turn is not in `session.get` yet.

   **Parked deliberately, and the reason matters.** This is a consequence of the
   design, not a slip: the design repaints from the core on switch — Task 8's
   fourth test asserts exactly that — rather than from a client buffer. Fixing
   it here would mean reintroducing the buffer the design rejected. The real
   answer is C2's append-only event log, where a transcript is derived from a
   replayable log and a switch is just a re-derivation, so there is no gap to
   fill. **Carry this into C2's design as a requirement.**

## Parked from the Task 9 review (both Minor)

10. **`notifyAsking` fires on any `onServerRequest` where the status is already
    `"asking"`, not only on the idle→asking transition.** Benign in the current
    design — one core request means one pending ask at a time in practice — but
    a second server request arriving for a project already mid-ask would
    overwrite `pendingAsk` and re-notify. The brief's own reference code has the
    same shape, so this is not a deviation.

11. **`ui/desktop/README.md` describes four chip states while an internal
    self-review table called the set "five".** No inconsistency with the code:
    `status` is a four-value enum and "active" is a separate boolean marker,
    not a status. Wording only, and inherited from the brief's verbatim text
    rather than introduced by the task.

## What the final review should weigh

- Items 1 and 2 together mean the rail is effectively mouse-only and loses
  focus under normal use. Consider whether that is acceptable for a shipped
  feature or should be fixed before merge.
- Item 9 is the one finding that should change a *later* design rather than this
  branch. It should not block the merge, but it must not be lost either.
- Items 3 and 4 are small, user-visible, and cheap.
- Items 10 and 11 are the smallest on this list and both were inherited from
  the brief's own text rather than introduced by an implementer.

## Parked from the final fix round's re-review (all Minor, none blocking)

12. **`sendTurn`'s `connFor(projectId).sendCancellable(...)` has no null guard**,
    where the deleted `wsSendCancellable` would have rejected gracefully. Safe by
    construction today — `projectId` is read from `currentProjectId`
    synchronously at function entry with no yield point before `connFor` — but a
    less defensive shape than what it replaced.

13. **The keyboard-opened rail menu does not move focus into itself.** A
    keyboard user must Tab away from the chip to reach "Close project" /
    "Remove from list". Not a regression: it mirrors the mouse path exactly
    (same function, no focus management there either), and the fix round was
    asked only for a keyboard path to the existing menu. The natural follow-up
    to parked item 2.

14. **`activeConn()` in `00-web-prelude.js` has zero callers** anywhere in
    `ui/web`; only `setActiveConn` is used. Pre-existing dead code. Note the
    fix round's report justified keeping it by pointing at the `active` variable
    it wraps — which `wsNotify` does read directly — but that is the variable,
    not the accessor. The accessor is dead.

15. **`closeProject` / `forgetProject` never explicitly hide the overlay.** If
    the only remaining project is closed while its own permission ask is on
    screen, the overlay stays visually up with dead buttons until the next
    switch. Safe by construction — `projectState()` lazily rebuilds an empty
    record for the deleted id, so a stray reply is dropped by the
    `!st.pendingAsk` guard — and identical in the pre-fix code, so neither
    introduced nor worsened here.

**Ruling on all four: parked, not fixed.** The branch has passed its final
gate. Adding unreviewed edits after that gate for cosmetic gain is the wrong
trade, even for a three-line dead-code deletion — the value of the gate is that
what ships is what was reviewed.

---

## Still unverified by hand

The owner ran C1 with several projects at once and confirmed parallel work
functions. Three things were never exercised in a real window, because nothing
in the build environment can see a Windows toast or press a button, and
inventing the observations would have been worse than leaving them open.

1. **The Windows notification.** Give a background project work that needs a
   permission answer and do not answer it: a toast should appear naming that
   project. The underlying call was proven to work from the served page during
   C1's first task (a real toast, observed via the window title); what is
   unverified is only this wiring.
2. **The two-asking-projects case.** Get two projects waiting on a permission
   prompt at the same time and switch between them. Each must show its own
   prompt, and answering must affect only that project. This was a Critical
   found by the whole-branch review — before the fix, the outgoing project's
   prompt stayed on screen and "Allow always" wrote a persistent rule into the
   other project. It has a regression test; it has never been clicked.
3. **The keyboard path to close/forget.** Tab to a chip, press `Shift+F10` or
   the ContextMenu key, and confirm the menu opens and both actions work. This
   has **no automated coverage at all** — the test harness's `closest()` stub
   returns null, so rail key and click handlers cannot be driven there. It was
   verified by reading the code only.

Worth recording why this matters rather than filing it as routine: two of the
three defects the whole-branch review found were visible from a single switch
between two projects. That check was skipped as "curl is close enough" during
implementation and deferred again afterwards, and both times it cost a full
review-and-fix cycle.
