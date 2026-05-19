# Audit-Ledger Comments

Many comments in the codebase reference audit items:

```go
// N4 in audit ledger (Sprint 6): without this, an assistant that opens
// three tool_calls but only receives two replies (network glitch, mid-
// batch error) would keep the orphan in history forever, hard-failing
// every subsequent LLM step with "tool_call_id not found".
```

## Why they exist

Several Sprint cycles (Sprints 1-6, plus the architecture audit) generated structured backlogs of issues (N1-N7, M1-M10, etc.). Comments tag the fix to its audit-item id so future maintainers can find the original problem statement in `docs/superpowers/plans/2026-05-19-post-audit-refactor.md` (and successor docs).

## Convention going forward

When adding a new audit reference, the tag MUST be followed by an explanation that stands on its own without reading the ledger. Bad:

```go
// H11 in audit ledger.   // ❌ what was H11?
```

Good:

```go
// H11 in audit ledger: task_result terminates the run rather than
// continuing so subagents can return a final string without the parent
// loop spinning waiting for one more step.
```

The tag is a back-reference, not the explanation. If the comment makes sense without the tag, the tag adds context for archeology; if it doesn't make sense without the tag, the tag is a tombstone and the comment needs a real explanation written.

## Cleanup

M3 in the architecture audit considered stripping the tags entirely. After review most existing comments DO carry explanations (the tags are prefixes, not the whole comment), so a mass strip would lose useful context. Future cleanup should be targeted per-comment: when reading a tag-only comment that adds no information, expand it with the invariant or remove it.
