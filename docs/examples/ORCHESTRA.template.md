# ORCHESTRA.md — project context for the agent

Edit this file for your repo. Orchestra injects it into every agent turn (`<project_memory>`).

## Stack

- Language / runtime:
- Build / test:
- LLM notes (num_ctx, prefer edit vs patches):

## Navigation (CKG-first — saves context)

Use the **narrowest** tool first:

1. **grep** — exact symbol/string, exhaustive match list
2. **glob** — file name patterns
3. **symbols** — definitions by name
4. **explore(package)** — package overview, no code bodies
5. **explore(Type.Method)** — one symbol + callers/callees + code
6. **read** — only when preparing a patch (need `file_hash`)
7. **semantic_search** — when grep fails (concept, not name)

**Never** read whole `.go` files for orientation — use `explore`.

## Editing strategy (automatic for local models)

You do **not** choose a CLI flag for this — the agent picks the path:

- **Default (small tasks):** `edit` / `write` tools → finish with `{"patches":[]}`
- **Rare (multi-file atomic):** `read` → `file_hash` → `final.patches`
- Never do both for the same change (StaleContent)

Local / Qwen / Llama family prompts enforce edit-first automatically (`prompt_family: local`).


## Memory

- Durable project facts: `memory_write` scope **project**
- This session only: `memory_write` scope **session** (symbol + file + line after root cause)
- Large memory on disk: **memory_read** layer `repo` or `session`

## Conventions

- Tests:
- Lint:
- Commit style:
