# Turn digest + working state (local token economy)

Rule-based context compression **without LLM**. Complements ModeCompaction / digests.
See also [`memory-context.md`](./memory-context.md).

## Goals

- Prefer **structure over prose** for local models
- Keep inject ≤ ~1–1.5 KiB combined
- Update every tool call; persist turn digest at end of `Agent.Run` and optionally every N steps
- **Never rewrite LLM history** for these artifacts (no full compact)

## `<working_state>`

In-memory ledger for the current turn, injected each `nextStep`:

```text
<working_state>
goal: …
files: a.go, b.ts
tools: read×3 edit×2
errors: undefined: Foo (a.go)
todos: [pending] fix tests; [completed] edit handler
</working_state>
```

Sources: tool names/paths from write/edit/read/bash; LSP error fingerprints; todos.

## `[turn_digest]` — per-turn / micro-summary

Appended to `.orchestra/memory/sessions/<id>.turns.md` (rule-based):

1. **End of Run** — final digest (no `step:` line)
2. **Every N steps** (`turn_digest_every_n`, default 6) — mid-run micro-summary with `step: N`

```text
---
[turn_digest]
step: 6
goal: …
done: edit a.go; fix tests
open: …
files: a.go, b.ts
tools: read×2 edit×1
errors: …
---
```

File is trimmed to the last **24** digests. Inject: last **`turn_digest_keep`** (default 3) into the user prompt as `<turn_digests>`.

History is **not** compacted by this path — only a local artifact + prompt inject.

## Policy

| Layer | LLM? | Cost |
|-------|------|------|
| working_state | no | free |
| turn_digest / micro | no | free |
| tool digest / prune | no | free |
| ModeCompaction | yes | expensive — last resort |

## Config

```yaml
agent:
  working_state: true          # default true
  turn_digest_keep: 3          # last N digests to inject (0 = off persist/inject)
  turn_digest_every_n: 6       # mid-run micro every N steps (0 = end-of-run only)
```
