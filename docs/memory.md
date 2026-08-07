# Memory system

Orchestra memory is **file-based** and optimized for **small-context local LLMs**: tiered inject, on-demand read, session vs project scope.

## Layers (priority)

| Layer | Path | Purpose |
|-------|------|---------|
| **orchestra** | `ORCHESTRA.md` | Committable project rules (like CLAUDE.md) |
| **session** | `.orchestra/memory/sessions/<session_id>.md` | Facts for one chat session |
| **repo** | `.orchestra/memory/*.md` | Durable agent notes (`agent.md` from `memory_write`) |
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

- **`memory_write`** — `{ content, scope?: "project"|"session" }` — prefix `[pin]` for sticky facts
- **`memory_read`** — `{ layer?, path?, max_kb? }` — list sources or read a layer
- **`memory_search`** — `{ query, limit? }` — substring search across memory layers

## Integration

- System prompt: `Agent.buildSystemPrompt()` → `memory.Store.FormatInject()`
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

- Semantic embeddings over memory (substring `memory_search` ships first)
- Full TUI memory editor
- Session-start hooks
