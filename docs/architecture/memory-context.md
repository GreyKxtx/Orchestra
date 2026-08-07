# Memory, context optimization, summarization

Канон слоёв памяти и компакции. Продуктовый обзор: [`docs/memory.md`](../memory.md).  
Как смотреть overflow / сравнение с Claude Code & OpenCode: [`context-overflow.md`](context-overflow.md). Roadmap phases ниже.

## Layers (do not mix)

```mermaid
flowchart TB
  subgraph durable [Durable file memory]
    Orch[ORCHESTRA.md]
    SessMD[".orchestra/memory/sessions/*.md"]
    AgentMD[".orchestra/memory/agent.md"]
    Global["~/.orchestra/memory.md"]
  end
  subgraph prompt [Per-step LLM prompt]
    Sys[System + FormatInject]
    Hist[Session History]
    Digests[Tool digests / prune]
    Compact[ModeCompaction checkpoint]
    CKG["ckg_context step1 only"]
  end
  subgraph ui [TUI only]
    UIMsg[UIMessages scrollback]
    CtxBar["ctx bar tokens%"]
  end
  durable --> Sys
  Hist --> Compact
  Compact --> Hist
  Digests --> Hist
  Hist --> CtxBar
  UIMsg -.->|"not compacted"| UIMsg
```

| Layer | Purpose | Code |
|-------|---------|------|
| File memory | Durable facts (project/session/global) | `internal/memory/` |
| LLM history | Working turn memory | `internal/agent`, `session.History` |
| Tool digests / prune | Shrink large tool outputs | `internal/agent/digest`, `history/prune.go` |
| Working state / turn digest | Rule-based ledger + end-of-turn digest (no LLM) | `internal/agent/working` |
| Compaction | LLM summarize history → checkpoint | `internal/agent/compact.go` |
| TUI scrollback | Human view | `UIMessages` — independent of LLM compact |

## Compaction as-is

**Trigger** (`shouldCompactHistory`): history bytes **or** estimated/real prompt
tokens exceed `compact_threshold_pct` of the prompt budget.

Budget is aligned with vLLM's wire check `prompt + max_tokens ≤ max_model_len`:
`PromptBudgetTokens(num_ctx, llm.max_tokens)` / `EffectiveMaxPromptBytes` (same reserve
in bytes). When the previous step's real `PromptTokens` already cannot leave room for
`max_tokens`, compaction fires immediately.

**Loop** (top of each agent step):
1. Retroactive prune (keep last N tool atoms full)
2. Soft path: digests already applied write-time
3. Hard path: `ModeCompaction` → `[Session checkpoint — structured summary]`
4. Guard: &lt;20% shrink or LLM fail → `truncateMessages`
5. Persist: session `ReplaceHistory(outHistory)` after turn (Phase 0)
6. Event `CONTEXT_COMPACTED` → TUI notice

**Sticky prefix** (Phase 1): checkpoint keeps original goal + recent tool atoms, not a single opaque blob.

**Usage trigger**: when last step reported real `PromptTokens`, prefer that over byte estimate.

## Session History invariant

After every `session.message` turn, `sess.History` **must equal** the agent `outHistory` (full rewrite). Append-only delta is wrong when compaction rewrites the prefix.

This includes **failed / soft-stopped turns** (`max_steps`, mid-turn errors that still return history). Previously `err != nil` skipped `ReplaceHistory`, so TUI `ui_sync` saved the chat while agent history stayed empty — reopen looked fine in UI but the model re-explored from scratch.

UIMessages stay full for the human; only LLM History is compacted.

## Gaps vs ideal (Cursor / Claude Code / OpenCode)

| Capability | Status |
|------------|--------|
| Tool digest + prune | Done |
| Structured in-prompt compact | Done |
| Persist compact across turns | Phase 0 |
| Sticky prefix + usage trigger + `/compact` | Phase 1 |
| Sticky facts / ModeSummary→memory / `memory_search` | Phase 2 |
| UI collapse old turns / `/memory` / ctx source | Phase 3 |
| Tokenizer calibration + metrics | Phase 4 |

## Phases

### Phase 0 — Persist
`Session.ReplaceHistory(outHistory)` after agent run; regression test multi-turn. **Done.**

### Phase 1 — Quality
Sticky prefix in `compactHistory`; usage-based trigger; TUI `/compact` force. **Done.**

### Phase 2 — Memory bank
Post-turn `ModeSummary` → project memory (`auto_summary_memory`); pinned facts (`[pin]`); `memory_search`. **Done.**

### Phase 3 — UX
Collapse old turns in chat view; `/memory` list/read; ctx bar `~` for estimate. **Done.**

### Phase 4 — Hardening
`bytes_per_context_token` config; `CompactMetrics` on agent. **Done.**

## Non-goals
- Mixing UIMessages into LLM compaction
- Replacing digests with LLM-only compact
- Separate vector DB for memory before reusing CKG embed path
