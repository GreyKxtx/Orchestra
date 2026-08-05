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

- **`memory_write`** — `{ content, scope?: "project"|"session" }`
- **`memory_read`** — `{ layer?, path?, max_kb? }` — list sources or read a layer

## Integration

- System prompt: `Agent.buildSystemPrompt()` → `memory.Store.FormatInject()`
- Lazy: `fs.read` → `Runner.discoverInstructions()` (capped ORCHESTRA.md)
- Session: `Core.SessionMessage` sets `session_id` on runner + agent options

## History optimization (OpenCode-style)

Configured under `agent:` and `embed:` in `.orchestra.yml`:

```yaml
agent:
  tool_digest_kb: 16             # digest tool outputs larger than this (default 16; -1 = off)
  history_prune_keep_recent: 2   # keep last N tool atoms full; older ones re-digested each step
  auto_session_memory: true      # auto-note explore/grep into session memory
  compact_threshold_pct: 70

embed:
  semantic_auto_explore: true    # semantic_search → auto explore(FQN) for top hits (default on)
  semantic_auto_explore_top_k: 2
```

- **Tool digest (write-time)** — large `read`/`grep`/`explore`/`bash`/`semantic_search`/`task` results become structured digests when appended to history
- **Retroactive prune** — at each agent step, older tool outputs in history are re-digested (last `history_prune_keep_recent` tool atoms stay full)
- **Structured compaction** — `ModeCompaction` prompt emits Goal/Decisions/Files/Next step sections
- **Subagent explore** — child runs with same digest/prune settings; parent receives one `[subagent:explore]` summary (Findings + Result), not raw tool spam
- **semantic_search pipeline** — top-K hits auto-enriched with `explore_summaries`; model gets FQN + callers preview in one call
- **CKG prefetch** — `<ckg_context>` only on **step 1** of each turn
- **Auto session memory** — one-line notes after explore/grep (session scope)

Template for project rules: [ORCHESTRA.template.md](./examples/ORCHESTRA.template.md)

## Not in v1 (future)

- Semantic retrieval / embeddings over memory
- Auto-summarization of long sessions into memory
- TUI memory editor
- Session-start hooks
