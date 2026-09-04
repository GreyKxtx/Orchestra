# Memory system

Orchestra memory is **file-based** and optimized for **small-context local LLMs**: tiered inject, on-demand read, session vs project scope.

## Layers (priority)

| Layer | Path | Purpose |
|-------|------|---------|
| **orchestra** | `ORCHESTRA.md` | Committable project rules (like CLAUDE.md) |
| **session** | `.orchestra/memory/sessions/<session_id>.md` | Facts for one chat session |
| **repo** | `.orchestra/memory/*.md` | Durable agent notes (`agent.md` from `memory_write`) |
| **lessons** | `.orchestra/memory/lessons/<dept>.md` | Episodic dept learning (L1 runtime + L2 `memory_write` scope=dept) |
| **global** | `~/.orchestra/memory.md` | User-wide preferences |

## Inject modes (`.orchestra.yml`)

```yaml
memory:
  inject_kb: 8          # eager <project_memory> cap (default 8)
  lazy_kb: 4            # ORCHESTRA.md cap in fs.read reminders
  mode: hybrid          # eager | lazy | hybrid (default hybrid)
  global_enabled: true
  session_enabled: true
  max_agent_kb: 128     # compact agent.md (keep recent entries)
```

- **eager** — every layer, every agent step (best when context is tiny and memory is small).
- **lazy** — minimal ORCHESTRA header + `memory_read` on demand.
- **hybrid** (default) — ORCHESTRA + session + recent `agent.md` entries. The global layer and any `.orchestra/memory/*.md` beyond `agent.md` stay behind `memory_read`.

Budget split: orchestra 35%, session 25%, repo 30%, global remainder — in hybrid the global share is folded into repo rather than wasted, so more recent `agent.md` entries fit. Entries are injected **recent-first**. `memory_read` layer `all` returns the full eager set whatever the mode, so nothing hybrid skips is unreachable.

## Embeddings (`embed:`)

```yaml
embed:
  provider: lmstudio        # named provider for api_base / api_key
  model: nomic-embed-text   # required — without it semantic_search is not offered at all
  auto_index: true          # default when model is set; false for paid endpoints
  batch_size: 32
```

Setting `embed.provider` without `embed.model` configures nothing: `semantic_search`
is registered only when a model is present. Orchestra warns about that pairing at
startup instead of leaving the tool silently missing.

`auto_index` builds the index in the background at core start, chained after the
CKG scan (there is nothing to embed until the graph has nodes). It is incremental,
so an unchanged repo costs no requests. `orchestra ckg embed [--rebuild]` runs the
same pass in the foreground.

## Tools

- **`memory_write`** — `{ content, scope?: "project"|"session"|<dept> }` — prefix `[pin]` for sticky facts. Dept scope (`engineering`, `frontend@web`, …) appends to `.orchestra/memory/lessons/<dept>.md` (max 3/run, 400 chars, deduped).
- **`memory_read`** — `{ layer?, path?, max_kb? }` — list sources or read a layer (`orchestra|session|repo|lessons|global|all`)
- **`memory_search`** — `{ query, limit? }` — hybrid search across memory layers (substring always; when `embed.model` is set, semantic re-ranking via shared embed client). When ranking was configured and the endpoint fails, the response carries `degraded` saying so rather than passing substring hits off as semantic ones.
- **`lesson_promote`** — `{ dept, note?, source? }` — Dept/Orchestra Lead only: draft local playbook overlay from last pattern lesson (`.orchestra/playbooks/local/<dept>.md`, `decision_ref: PENDING:…`)
- **`playbook_promote`** — `{ dept, promotion_ref? }` — merge approved local overlay into L2 `.orchestra/playbooks/<dept>.md`; `promotion_ref` optional — defaults to overlay `decision_ref` (single User approval in `decisions.md`). Local overlay file removed after merge.

### Learning stack (L0–L3)

| Level | Mechanism | Path |
|-------|-----------|------|
| L0 | working_state, turn_digest | agent history |
| L1 | Episodic lessons after worker verify, **and after any top-level turn that ended with errors** | `.orchestra/memory/lessons/<dept>.md` |
| L2 | `memory_write` scope=dept | same lessons files (`agent_note`) |
| L3 inject | `<dept_playbook>` | L2 playbook + local overlay |
| L3 write | local overlay gate | `decision_ref` in `decisions.md` (auto-sealed via Question Barrier) |

Signals: repeated anti-patterns (3×) → `lesson_promote_suggestion` in worker `task_result`. After overlay approval → `playbook_promote_suggestion`.

**Single-agent modes learn too.** `build`, `debug`, `plan`, `ask` and `architecture` write an anti-pattern lesson under `engineering` when a turn ends with errors still on its ledger, and replay `<dept_lessons>` in the next session's system prompt. Previously only worker children recorded anything, so the mode most people run learned nothing from itself — over a 52-session field run the lessons directory was never created. Child-only modes are skipped here because their spawner already gives them dept-scoped lessons; one-shot `apply` runs are skipped because they have no session to learn into.

Explore-first gate blocks `write`/`edit` for Worker, Orchestra Lead, and Dept Lead (`architecture`) until `read`/`grep`/`explore` on scope.

## Integration

- System prompt: `Agent.buildSystemPrompt()` → `memory.Store.FormatInject()`
- Orchestra Lead: `lessons.FormatLeadInject` (≤5 entries/dept, ~1000 tokens) + `playbooks.FormatLeadPlaybooksInject` (active dept or filename index, ~1000 tokens; combined ≤2000 tokens)
- Workers: dept-scoped `<dept_lessons>` + `<dept_playbook>` at spawn (not the Lead catalog)
- Lazy: `fs.read` → `Runner.discoverInstructions()` (capped ORCHESTRA.md)
- Session: `Core.SessionMessage` sets `session_id` on runner + agent options; **ReplaceHistory** persists compaction
- TUI: `/compact` → `session.compact`; `/memory` lists layers + pins

## History optimization (OpenCode-style)

Configured under `agent:` and `embed:` in `.orchestra.yml`:

```yaml
agent:
  tool_digest_kb: 16
  history_prune_keep_recent: 6
  auto_session_memory: true    # explore auto-notes into the session file
  auto_summary_memory: true    # default; every changing turn → agent.md note
  working_state: true          # rule-based <working_state> (no LLM)
  turn_digest_keep: 3          # last N digests; 0 = off
  turn_digest_every_n: 6       # mid-run micro-summary every N steps; 0 = end only
  compact_threshold_pct: 0     # 0 = auto (window-derived); -1 = off
  bytes_per_context_token: 4
  child_max_steps: 24
```

### What reaches `agent.md`, and when

After every turn `auto_summary_memory` writes one note to project memory, from
whichever source is available:

1. **ModeSummary prose** — a short LLM summary, when the endpoint answers and
   the turn has at least 4 messages of history.
2. **Turn digest note** — rule-based, no LLM: `goal` / `done` / `files` lifted
   from the digest the agent already persisted for this turn.

The second path exists because the first is the fragile one. A field run over
52 sessions left a single durable note: the configured endpoint was down for a
day (183 `llm_error` events) and every summary call failed quietly to stderr.
The digest is on disk before the summary is attempted, so an outage no longer
costs the memory of the run.

A note is written only when the turn **changed** something — `done:` is filled
from `write` / `edit` / `bash`. Turns that only read, grepped or explored write
nothing, since `files:` alone would put every look-around turn in `agent.md`.

`explore` still auto-notes into the *session* file. `grep` does not: a match
count describes one search, not the project, and those lines were most of what
the session files actually held.

### Context window and the compaction trigger

`compact_threshold_pct: 0` (the default) scales the trigger to the model's real
context window — 60% under 32k tokens, 75% under 100k, 85% above. Set a
positive value to pin it.

The window itself comes from `llm.ResolveModelLimits`: the server's
`/v1/models` (`max_model_len` / `max_context_length`) when it reports one, else
the static family catalog in `llm/model_context.go`. Cloud providers report
nothing, so without the catalog the budget fell back to the flat
`limits.context_kb` (128 KB ≈ 30k tokens) on a 200k-token model. Every entry
point that builds a client from config resolves this — `core` and `apply` alike.

Compaction summarises only the **older** part of the history: the recent tail
(30% of the prompt budget, at least `history_prune_keep_recent` tool atoms)
stays in the transcript verbatim.

Rule-based digests: [`architecture/turn-digest-working-state.md`](./architecture/turn-digest-working-state.md).
Architecture + phases: [`architecture/memory-context.md`](./architecture/memory-context.md).

## Not in v1 (remaining)

- Full TUI memory editor
- Session-start hooks
