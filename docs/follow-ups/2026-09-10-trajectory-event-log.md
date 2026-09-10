# Follow-ups: the trajectory event log (C2a)

Findings raised during C2a's reviews that were judged real and parked with a
stated reason rather than fixed. None is a bug report awaiting triage — each was
seen, weighed and left deliberately, and the reasoning is the useful part.

C2a shipped in `feat/trajectory-event-log` (13 commits) after four task reviews,
two fix rounds and a whole-branch review. What was fixed is not listed here.

Spec: `docs/superpowers/specs/2026-09-09-multi-project-window-design.md`
Plan: `docs/superpowers/plans/2026-09-10-trajectory-event-log.md`

**What C2a delivered, so the parked items read in context:** an append-only
per-session event log at `.orchestra/sessions/<id>.events.jsonl`, written by the
core as a turn happens, read back over `session.trajectory`. The session snapshot
schema stays at v4 — the log is a sidecar, which is why no existing session
needed migrating. C2b builds the view that reads it.

---

## Parked from the Task 1 review

1. **The pin test's regexp takes the first textual `PROTOCOL_VERSION = <n>`.**
   `internal/cli/protocol_pin_test.go` greps `ui/vscode/src/coreSession.ts` from
   Go, so a commented-out or duplicated earlier assignment would be read instead
   of the live one. Parked: it catches the only failure that has actually
   happened — a bare constant left behind at an old number, which is exactly how
   the extension came to sit at 14 while the core was at 15 — and the
   alternative, parsing TypeScript from Go, is out of proportion. The failure
   messages are good: a renamed or missing constant fails loudly naming the file,
   and a mismatch names both numbers.

2. **`internal/sessionfile/migrate.go`'s error string says "parse v2"** when the
   branch it guards now covers v2 through v5 and beyond. Pre-existing — it
   already misreported for v4 documents before this change — so not a
   regression, but it will mislead whoever next reads a migration failure.

## Parked from the Task 2 review

3. **`lastSeq` rescans the whole sidecar on every `NewWriter`.** Once per
   writer, not per append, so a turn pays it once. The cost grows with the
   session's length, and nothing trims the log. Parked as proportionate for now;
   worth re-rating when C2b makes long sessions with long logs the normal case.

4. **A corrupt line in the middle of a log is dropped as silently as an expected
   torn tail** — no count, no signal, no way for a reader to know it happened.
   `Read` skipping any unparseable line is deliberate and should stay: a log
   outlives the code that reads it, and one bad line must not deny a reader the
   other thousand. What is parked is the *observability* half. Closing it means
   changing `Read`'s signature to report how many lines it skipped, and
   `session.trajectory` already depends on that signature.

5. **`TestAppend_AFailedWriteDoesNotReissueItsSequenceNumber` is a
   characterization test, not a regression test.** It passes against the
   pre-fix code: the test closes the file handle before the second `Append`, so
   the write fails without putting any bytes on disk and there is never a partial
   line to mishandle. It was specified as fail-first and is not; the implementer
   caught this and said so. Kept because it pins the invariant against future
   edits and is the only coverage of the `tornWrite` flag. A genuine partial write
   cannot be manufactured without making `Writer.f` an injectable `io.Writer`,
   which is a refactor this feature did not need.

## Parked from the Task 3 review

6. **The two `prepareAgentLaunch` error paths that could leak the writer are
   unreachable today.** `:233` and `:354`. The fix — a `defer` keyed on the named
   error return — stays as defence for error paths the function grows later,
   which is why it was chosen over closing at the two known sites. Unreachability
   was verified three ways: `validateAgents`
   (`internal/config/config.go:1357-1362`, called from `Validate` at `:1084`)
   rejects the only config that reaches `:233`; `AgentsUpsert` enforces the same
   rule at runtime (`internal/core/runtime_agents.go:95`); and no RPC deletes a
   provider, so an agent can never come to reference one that is absent. Recorded
   because a future provider-delete API would make the path live.

7. **The leak test builds its situation by hand.** Because of item 6 it cannot
   provoke the failure through configuration, so it constructs the core with no
   injected LLM client and appends to `c.cfg.Agents` in memory, bypassing the
   validation every real entry point enforces. Precedent exists in the package
   (`hooks_lifecycle_test.go:78`). It pins the mechanism, and the mechanism is
   real even though today's path to it is not.

## Carried out of Task 4's review — the one to watch

8. **`ui/desktop/src-tauri/src/boot.rs` parses and stores a `protocol_version`
   and never compares it to anything.** Confirmed by grep: no gating logic
   exists, and the literal `15`s nearby are test fixtures. Harmless today,
   because nothing reads it.

   It is on this list because **it is the same shape as the bug Task 1 existed to
   fix.** The VS Code extension pinned 14 while the core was at 15, shipped that
   way, and told users to rebuild `orchestra.exe` — pointing away from the real
   cause. Nothing compared the two numbers, so nothing noticed. A stored-but-
   unchecked `protocol_version` in the desktop shell is a pin waiting to be
   wired up at whatever number it happens to hold.

   **When Part D (packaging) lands, either compare it or delete it.** If the
   desktop shell should reject a mismatched core, it needs the check and a
   companion to `TestVSCodeExtensionPinsCurrentProtocolVersion`. If it should
   not, the field should go, so nobody wires it up later believing it was
   already meaningful.
