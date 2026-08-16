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

- **eager** — full tiered inject every agent step (best when context is tiny and memory is small).
- **lazy** — minimal ORCHESTRA header + `memory_read` on demand.
- **hybrid** (default) — ORCHESTRA + session + recent `agent.md` entries; rest via `memory_read`.

Budget split in eager/hybrid: orchestra 35%, session 25%, repo 30%, global remainder. `agent.md` entries are injected **recent-first**.

## Tools

- **`memory_write`** — `{ content, scope?: "project"|"session"|<dept> }` — prefix `[pin]` for sticky facts. Dept scope (`engineering`, `frontend@web`, …) appends to `.orchestra/memory/lessons/<dept>.md` (max 3/run, 400 chars, deduped).
- **`memory_read`** — `{ layer?, path?, max_kb? }` — list sources or read a layer (`orchestra|session|repo|lessons|global|all`)
- **`memory_search`** — `{ query, limit? }` — hybrid search across memory layers (substring always; when `embed.model` is set, semantic re-ranking via shared embed client)
- **`lesson_promote`** — `{ dept, note?, source? }` — Dept/Orchestra Lead only: draft local playbook overlay from last pattern lesson (`.orchestra/playbooks/local/<dept>.md`, `decision_ref: PENDING:…`)
- **`playbook_promote`** — `{ dept, promotion_ref? }` — merge approved local overlay into L2 `.orchestra/playbooks/<dept>.md`; `promotion_ref` optional — defaults to overlay `decision_ref` (single User approval in `decisions.md`). Local overlay file removed after merge.

### Learning stack (L0–L3)

| Level | Mechanism | Path |
|-------|-----------|------|
| L0 | working_state, turn_digest | agent history |
| L1 | Episodic lessons after worker verify | `.orchestra/memory/lessons/<dept>.md` |
| L2 | `memory_write` scope=dept | same lessons files (`agent_note`) |
| L3 inject | `<dept_playbook>` | L2 playbook + local overlay |
| L3 write | local overlay gate | `decision_ref` in `decisions.md` (auto-sealed via Question Barrier) |

Signals: repeated anti-patterns (3×) → `lesson_promote_suggestion` in worker `task_result`. After overlay approval → `playbook_promote_suggestion`.

Explore-first gate blocks `write`/`edit` for Worker, Orchestra Lead, and Dept Lead (`architecture`) until `read`/`grep`/`explore` on scope.

## Integration

- System prompt: `Agent.buildSystemPrompt()` → `memory.Store.FormatInject()`
- Orchestra / Architecture Lead: also `lessons.FormatLeadInject` + `playbooks.FormatLeadPlaybooksInject` (cross-session L1 lessons + L2 playbooks / local overlays)
- Workers: dept-scoped `<dept_lessons>` + `<dept_playbook>` at spawn (not the Lead catalog)
- Lazy: `fs.read` → `Runner.discoverInstructions()` (capped ORCHESTRA.md)
- Session: `Core.SessionMessage` sets `session_id` on runner + agent options; **ReplaceHistory** persists compaction
- TUI: `/compact` → `session.compact`; `/memory` lists layers + pins

## History optimization (OpenCode-style)

Configured under `agent:` and `embed:` in `.orchestra.yml`:

```yaml
agent:
  tool_digest_kb: 16
  history_prune_keep_recent: 2
  auto_session_memory: true
  auto_summary_memory: false   # ModeSummary → project agent.md after long turns
  working_state: true          # rule-based <working_state> (no LLM)
  turn_digest_keep: 3          # last N digests; 0 = off
  turn_digest_every_n: 6       # mid-run micro-summary every N steps; 0 = end only
  compact_threshold_pct: 70
  bytes_per_context_token: 4
```

Rule-based digests: [`architecture/turn-digest-working-state.md`](./architecture/turn-digest-working-state.md).
Architecture + phases: [`architecture/memory-context.md`](./architecture/memory-context.md).

## Not in v1 (remaining)

- Full TUI memory editor
- Session-start hooks
