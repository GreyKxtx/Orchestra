# Changelog

All notable changes to Orchestra are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

---

## [Unreleased] — vNext

### Added — Attachments & vision (protocol v13)

Multimodal user messages: images (PNG/JPEG/GIF/WebP), SVG, PDF; staging under `.orchestra/attachments/`; RPC `session.message` / `agent.run` param `attachments[]`.

- **`internal/attachments`** — path validation, MIME detection, workspace-safe staging copy.
- **TUI** — `/attach <path>`, chips in user bubble, persist in session v4 `UIMessage.attachments`.
- **VS Code extension** (`ui/vscode/`) — drag-drop / picker, open attachment or diff in workspace editor (not webview), LSP install modal, per-file diff review (↑↓ a x Enter), `@`-mention without webview reload.
- **Session roundtrip** — `llm.Message` JSON unmarshals multimodal `parts` for reload.
- **E2E** — `tests/e2e_real_llm/vision_test.go` (gated by `ORCH_E2E_LLM=1`).

### Changed — Phase 0 stabilization

- **`code.symbols`** — documented three-tier resolution (LSP → tree-sitter when CGO → regex); method vs function kinds in regex fallback; CGO integration test.
- **`orchestra core --http`** — explicitly debug-only in CLI help and stderr notice; stdio remains the supported transport.

### Changed — Phase 1 local-model hardening ✅

- **Provider-aware retry limits** — `FillRetryLimits` / `RetryLimitsForProvider`; config `0` = auto (frontier → 1, local → 5). Wired in core, apply, pipeline.
- **`ResolveResponseFormat`** — shared helper; **auto `json_schema` for local providers** unless `supports_json_schema: false`.
- **Prompts** — deprioritize `file.unified_diff` in build-gpt/gemini/kimi; per-family templates via `internal/prompt/files/*-{family}.txt`.
- **Eval harness** — 5 Phase-1 tasks (`rename_func`, `add_func`, `fix_bug`, `add_test`, `refactor`); `ParseLLMLog` metrics; `orchestra eval` RETRIES column + avg summary.

- **`call_dispatch.go`** — table-driven tool dispatch via `toolDispatchTable` + generic `dispatchRunnerTool`.
- **Test colocation** — browser tests moved to `internal/tools/web/browser_test.go`; root duplicates removed.
- **Removed `orchestra daemon` CLI** — `internal/cli/daemon.go` and daemon client discovery in `orchestra search`; `internal/daemon` package kept for in-repo benchmarks only.


`internal/core` split: `runtime_agents.go`, `runtime_llm.go`, `runtime_mcp.go`, `runtime_providers.go`, `runtime_prompt.go`, `runtime_index.go`, `message_attachments.go`. CI adds `vscode-extension` job (`npm ci` + `compile`).

### Changed — Phase 0 module layout (`internal/uimodel`)

Neutral chat DTOs moved to **`internal/uimodel`**. `internal/sessionstore` no longer imports `ui/tui/state`. Layer rules: `docs/architecture/modules.md`.

### Changed — Phase 4 tools subpackages (`internal/tools/*`)

Split tool implementations into subpackages while keeping the registry and public `tools` API at package root:

- **`internal/tools/exec/`** — `Run`, background bash registry, tool defs (`ToolExecRun`, …)
- **`internal/tools/git/`** — git + `gh` CLI tools, tool defs
- **`internal/tools/web/`** — `webfetch`, `websearch`, Playwright browser tools, tool defs
- **`internal/tools/toolslsp/`** — LSP tool implementations + defs (avoids clash with `internal/lsp`)
- **`internal/tools/toolpath/`**, **`internal/tools/toolschema/`** — shared path + JSON-schema helpers
- **`internal/tools/fs/`** — filesystem tools (`Client`, `Overlay`, staging, list/read/write/edit/glob/grep, delete/rename, diff.preview, ast_rename) + tool defs
- Root **`registry.go`** — `ListTools*` / `allToolDefsMap` / parallel flags; delegates to subpackage `Tool*` constructors
- Root **`aliases.go`**, **`*_delegate.go`** — backward-compatible types and `Runner` methods

### Changed — Phase 4b tools nav/session (`internal/tools/nav`, `internal/tools/session`)

- **`internal/tools/nav/`** — explore, symbols, semantic_search, repo_map, CKG admin; tool defs in `nav/registry.go`
- **`internal/tools/session/`** — todo types/validation, memory_*, runtime_query, question; tool defs in `session/registry.go`
- Root **`nav_delegate.go`**, **`session_delegate.go`**; monolith **`registry.go`** delegates tool defs to subpackages

### Changed — Phase 4c tools task subpackage (`internal/tools/task`)

- **`internal/tools/task/`** — subagent tools (`task`, `task_spawn/wait/cancel/result`), plan mode (`plan_enter/exit`), `ToolSkillInvoke`
- Removed root **`path_shim.go`**; skill tests moved to `task/skill_invoke_test.go`

### Changed — Phase 6 tools cleanup (partial)

- Split **`internal/tools/call.go`** → `call_dispatch.go`, `call_decode.go`
- Moved tests: **`git/github_test.go`**, **`web/webfetch_ssrf_test.go`**, **`session/memory_write_test.go`**
- **`internal/daemon/doc.go`** — explicit deprecated package doc


- **`tests/importrules/`** + CI job — enforce layer import direction from `modules.md`
- **`internal/core/session_rpc.go`** — Session JSON-RPC surface extracted from `core.go`
- **`internal/core/core_agent.go`** — `AgentRun`, `ToolCall`, usage/MCP/custom-agent helpers
- **`internal/agent/tool_parallel.go`** — parallel tool batch + JSON error/denial helpers
- **`internal/agent/agent_run.go`**, **`agent_step.go`**, **`agent_prompt.go`** — split from monolith `agent.go`

### Changed — Phase 3 LLM module (`llm/`)

Extracted **`github.com/orchestra/orchestra/llm`** (clients, streaming, catalog, `LLMConfig`/`RouterConfig`/`ModelPreset`, `lmstudio/`). `internal/config` type-aliases LLM types; `ProjectConfig.LLMRegistry()` feeds provider resolution without config↔llm import cycle.

### Changed — Phase 2 patch module (`patch/`)

Extracted **`github.com/orchestra/orchestra/patch`** (`ops`, `patches`, `resolver`, `applier`, `fsutil`, `cache`, `relpath`); depends on `protocol/`; added to `go.work`.

### Changed — Phase 1 wire module (`protocol/`)

Extracted **`github.com/orchestra/orchestra/protocol`** sub-module (`protocol`, `jsonrpc`, `schema`); root **`go.work`**; main module `replace => ./protocol`.

### Added — Skill packs: install/uninstall + per-skill review (2026-05-18)

Third-party skill bundles can be installed from git URLs, HTTP(S) zip/tar archives, or local directories. Installed packs live under `~/.orchestra/packs/<id>/`; `Discover` adds a third tier (project > user > pack) and tags each skill with its `Origin`.

**Security gate** — a skill body becomes a system prompt for a child agent with full tool access. Install therefore prints every skill's metadata + full body and prompts `y/N` per skill. `--yes` bypasses the prompt with a visible warning.

- `internal/packs/` — `ParseSource` (git/http/local), `Fetch` (git clone --depth 1, http archive download with zip + tar/tar.gz support and zip-slip guard, local recursive copy that skips .git and symlinks). Deterministic `Source.ID()` via sanitised name + SHA1 prefix.
- `internal/skills/` — `Skill.Origin` field; `DiscoverFromAll(packsRoot, userDir, projectDir)`; project > user > pack precedence; per-pack duplicate-name check; recursive `*.md` scan inside each pack.
- `orchestra skills install <source> [--yes]` — fetch, validate, per-skill review.
- `orchestra skills uninstall <pack-id>` — `rm -rf` the pack dir.
- `orchestra skills list` now has an `ORIGIN` column (`project` / `user` / `pack:<id>`).
- Out-of-scope follow-ups: `update` (git pull + re-confirm changed skills), version pinning, integrity hashes, signature verification.

### Added — Image input: --image flag + browser.screenshot pipe (2026-05-18)

The agent now accepts images. Two entry points:

1. **CLI: `orchestra apply --image foo.png "what's wrong here?"`** — repeatable flag, supports PNG/JPEG/GIF/WebP, attached to the first user message as multimodal Parts.
2. **`browser.screenshot` pipe** — when the configured LLM is multimodal, the base64 PNG returned by `browser.screenshot` is automatically injected as a `PartImage` in a synthetic follow-up user message, so the model can actually "see" the page on the next step.

- `llm.Message` gains `Parts []ContentPart`; when non-empty, OpenAI-compatible clients serialise the array form (`[{type:"text"}, {type:"image_url"}]`) via a custom `MarshalJSON`. Existing text-only path unchanged.
- `llm.ContentPart{Kind, Text, ImageURL, ImageData, ImageMIME}` — PartImage accepts either a remote/data URI or raw bytes (the client builds the data URI).
- `Message.TextLen()` + `HasImages()` — compaction/truncation count text bytes only; image parts get a fixed 4 KB per-part budget so massive base64 doesn't dominate `estimateMessageSize`.
- `config.LLMConfig.Multimodal bool` — must be set true for image flows to fire; CLI fails fast on `--image` against a text-only LLM.
- `agent.Options.UserImages []llm.ContentPart` + `MultimodalLLM bool` — agent gates both initial-message images and screenshot piping on the flag.
- Anthropic encoding, TUI clipboard paste, and other multimodal tool returns are documented follow-ups.

### Added — Semantic search over CKG (sub-project 4 of CKG roadmap, 2026-05-18)

Long-tail concept search complements text grep and CKG explore. Embed all
function/method/type nodes, then query by natural-language concept:

- `internal/embed/` — OpenAI-compatible embeddings HTTP client (works
  with OpenAI, Ollama, LM Studio, Voyage) + cosine similarity helper.
- `internal/ckg/embed_store.go` — `node_embeddings(node_id, model, dim,
  vector BLOB)` table; `SaveEmbeddings`/`SearchSimilar`/`MissingEmbeddings`
  with brute-force cosine. Schema bumped v3→v4 (drop+recreate as before,
  local cache rebuilds).
- `orchestra ckg embed` (`--rebuild`, `--limit`, `--batch-size`) — reads
  the source range of every indexable node and embeds it in batches.
- `semantic_search {query, top_k, snippet}` tool — embeds the query,
  returns top-K nearest CKG nodes (`fqn`, `kind`, `path`, line range,
  cosine score, optional snippet). Registered only when `embed.model`
  is set in `.orchestra.yml`.
- New config block: `embed: {api_base, api_key, model, dimensions,
  batch_size, timeout_s}`.

Closes CKG runtime roadmap sub-project 4 (Vector DB / semantic search).

### Added — Background bash: run_in_background, bash.output, bash.kill (2026-05-18)

Long-running commands (build, test loops, dev servers, watch processes) no longer block the agent. The existing `bash` tool gains an optional `run_in_background: true` parameter; when set, it returns immediately with a `bg_id` instead of waiting for completion.

- `bash {run_in_background: true, command, args, workdir, timeout_ms}` — spawns under context cancellation, registers in a per-Runner registry, returns `{bg_id, status, command, started}`.
- `bash.output {bg_id, peek}` — returns *new* stdout/stderr since the last poll (per-process cursor), plus current status (`running`/`done`/`killed`/`timed_out`/`error`), exit code when finished, and per-stream truncation flags. `peek: true` reads without advancing the cursor.
- `bash.kill {bg_id}` — terminates the process; no-op (returns current status) when already finished.
- Per-stream buffer cap: 256 KB (truncates oldest content, flagged in response).
- `Runner.Close()` kills every still-running background process — no leaks between runs.
- All three tools gated by `--allow-exec` (same as `bash` itself).
- No protocol/tools version bump: pure additive extension, same `bash` schema with one new optional field.

### Added — Skills follow-ups: user-global, $ARGUMENTS, skill_invoke (2026-05-18)

- `Discover` now scans both `<userHome>/.orchestra/skills/` and `<project>/.orchestra/skills/`; project skills override user skills with the same `name`. Internal: `DiscoverFrom(userDir, projectDir)` is the testable entry point.
- `$ARGUMENTS` in a skill body is replaced with the user query (`--skill` flow) or with the `task` parameter (`skill_invoke` flow). Skills without the marker are unchanged.
- New `skill_invoke` tool: when any skill is discovered, every `apply` run exposes `skill_invoke{skill, task}` to the model and advertises available skills in the system prompt via a `<available_skills>` block. The handler runs a fresh child agent synchronously with the skill's prompt + tool filter + model + provider, returning its result. Child runs with no SubtaskRunner / SkillRunner (no recursion).
- `internal/cli/skill_runner.go` — `cliSkillRunner` implementing `agent.SkillRunner`; resolves per-skill LLM client via `cfg.FindProvider` + `llm.NewClient`, mirroring the `--skill`/`--agent` model/provider override semantics.
- `internal/tools/registry.go` — `ToolSkillInvoke(names)` builds the JSON-Schema with an `enum` of allowed skill names (when supplied), tagged Mutating.
- No ToolsVersion bump: `skill_invoke` is registered dynamically per-run (only when skills exist) and is dispatched in-process — it never crosses the JSON-RPC tools surface.

### Added — Skills loader (2026-05-17)

File-based, discoverable agent skills loaded from `<project>/.orchestra/skills/*.md`. A skill is a Markdown file with a YAML frontmatter header (`name`, `description`, `tools`, `model`, `provider`) and a Markdown body used as the agent system prompt. Skills are the shareable, file-based form of the inline `agents:` block in `.orchestra.yml` — same merge semantics into `agent.Options`.

- `internal/skills/` — `Skill` struct, `Parse(source, r)`, `Load(path)`, `Discover(projectRoot)`, `Find`. Discovery validates required `name` and rejects duplicate names across files.
- `orchestra skills list` — prints discovered skills as a NAME/DESCRIPTION table.
- `orchestra skills show <name>` — prints full metadata + prompt body.
- `orchestra apply --skill <name> "<task>"` — runs apply with the skill's prompt, tool filter, model and provider overrides. Mutually exclusive with `--mode`.
- Tool names in a skill's `tools:` list are validated against the same allow-list used by inline `agents:` (new exported `config.ValidAgentTool`).
- No protocol/tools version bump — skills are a CLI-side loader on top of existing `agent.Options`; the LLM tool surface is unchanged.

See `docs/skills.md`.

### Added — Staging overlay: dry-run write/edit safety (2026-05-15)

До этого `write`/`edit` писали напрямую на диск даже в режиме `--plan-only`, обходя `--apply`-флаг. Теперь в dry-run режиме все записи накапливаются в памяти (overlay) и на диск не попадают.

#### Архитектура (Variant C — full staging)

- **`internal/tools/staging.go`** (новый файл) — `stagedFile`, методы `stageFile`, `stagedContent`, `currentHash`, `StagedOps`, `ApplyPatchesToStaged`, `ClearStaged`, `HasStagedChanges`. `sync.RWMutex` — read-методы используют `RLock`.
- **`internal/tools/runner.go`** — поля `dryRun bool`, `staged map[string]*stagedFile`, `stagedMu sync.RWMutex`; опция `RunnerOptions.DryRun`; метод `SetDryRun`; в `FSRead` — overlay-check перед чтением диска.
- **`internal/tools/write.go`** — dry-run ветка: проверяет `must_not_exist` и `file_hash` в overlay, пишет в `staged`.
- **`internal/tools/edit.go`** — dry-run ветка: читает из overlay или диска, применяет search/replace через `resolver.ApplySearchReplace`, пишет результат в `staged`.
- **`internal/agent/agent.go`** — `StepFinal` разделён на два пути:
  - **DRY-RUN:** `ApplyPatchesToStaged(finalPatches)` → `StagedOps()` → `FSApplyOps(DryRun=true)`. `plan.json` получает `write_atomic` ops с `diskHash`-условиями для stale-detection при `--from-plan`.
  - **APPLY:** старый путь `ResolveExternalPatches` → `FSApplyOps(DryRun=false)`.
- **`internal/cli/apply.go`** — `DryRun: dryRun` передаётся во все три `NewRunner`-вызова (from-plan, via-core, direct).
- **`internal/core/core.go`** — перед каждым `agent.Run`: `SetDryRun(!params.Apply)` + `ClearStaged()`.

#### Что проверяется

- `--plan-only` создаёт `plan.json` с `ops` (не пустой) и НЕ изменяет файлы на диске.
- `--from-plan --apply` применяет план; повторный вызов даёт `StaleContent` без побочных эффектов.
- `TestRealLLMMinimalFlow` и `TestStaleScenario` — оба проходят с реальной моделью.

---

### Added — LSP включён + полное тестовое покрытие инструментов (2026-05-15)

#### LSP (gopls)

- **`.orchestra.yml`** — добавлена секция `lsp.servers` с gopls: `command: ["gopls", "serve"]`, extensions `[".go"]`, `diagnostics_timeout_ms: 5000`. Агент теперь может использовать все 5 LSP-инструментов на реальном gopls.

#### Новые тесты

**`internal/tools/webfetch_test.go`:**
- Пустой URL, неверная схема (`ftp://`)
- SSRF блокировка: `127.0.0.1` (loopback), `10.x` (private A), `192.168.x` (private C), `[::1]` (IPv6 loopback), `169.254.x` (link-local)
- HTML extraction (`extractTextFromHTML`) — script-теги вырезаются, `<title>` извлекается
- `isLikelyHTML` — 6 cases (html/doctype/head/body vs JSON/plain text)

**`internal/tools/lsp_tools_test.go`** (с реальным gopls, skip при `-short`):
- `TestLSP_NoServerConfigured_ReturnsError` — без конфига → "no servers configured"
- `TestLSP_Definition` → возвращает позицию объявления функции `Greet`
- `TestLSP_Hover` → возвращает сигнатуру и doc-комментарий
- `TestLSP_References` → 2 локации (объявление + вызов)
- `TestLSP_Diagnostics_ValidFile` → 0 error-диагностик на корректном файле
- `TestLSP_Diagnostics_BrokenFile` → compiler error на type mismatch
- `TestLSP_Rename` → edits для переименования `Add` → `Sum`

#### Покрытие инструментов после этой сессии

Все инструменты покрыты тестами. Единственный пробел — `question` (интерактивный stdin, тест невозможен без рефакторинга).

---

### Added — Parallel tool execution (2026-05-13)

The agent now executes ParallelSafe tool calls (read-only: `ls`, `read`, `glob`, `grep`, `symbols`, `explore`, `lsp.*`, `todoread`, `task.result`, `runtime.query`, `webfetch`) **concurrently** when the model emits several in a single response. On read-heavy "analyze the project" turns this collapses what used to be 10-20 sequential LLM round-trips into a single batch with a worker-pool fan-out — typically 5-10× speedup.

#### `llm.ToolDef` — declarative concurrency flags

- **`ParallelSafe bool`** — pure reads with no shared-state risk; can run alongside other ParallelSafe tools in one batch.
- **`Mutating bool`** — has observable side effects (file writes, shell commands); always runs one-at-a-time.

Both fields are in-process metadata (`json:"-"`) — the wire format is unchanged.

#### `internal/tools/registry.go` — central classifier

- **`applyParallelFlags(defs []llm.ToolDef) []llm.ToolDef`** — switch keyed on tool name; called by every `ListToolsXXX` constructor. Single source of truth — adding a new tool means one switch update, not edits across every list function.
- Read-only set (ParallelSafe): `ls`, `read`, `glob`, `grep`, `symbols`, `explore`, `todoread`, `task.result`, `runtime.query`, `webfetch`, `lsp.definition`, `lsp.references`, `lsp.hover`, `lsp.diagnostics`.
- Mutating set: `write`, `edit`, `bash`, `todowrite`, `memory_write`, `lsp.rename`, `plan.enter`, `plan.exit`, `task.spawn`, `task.wait`, `task.cancel`, `question`.

#### `internal/agent/types.go` — `Step.Tools`

- **`Step.Tools []ToolCall`** — parallel batch slot. Populated by `NormalizeLLMWithDefs` only when the response carries ≥2 calls **and** every one is ParallelSafe.
- **`Step.Tool *ToolCall`** — preserved for the legacy single-tool serial path; also used as fallback when a batch mixes read-only with mutating (we execute the first call and drop the rest).
- **`ToolCall.ID string`** — now propagated through the type so per-tool replies use the original `tool_call_id`.

#### `internal/agent/step_adapter.go`

- **`NormalizeLLMWithDefs(v, resp, defs []llm.ToolDef)`** — new flag-aware variant. Picks parallel-batch path when `allParallelSafe(calls, defs)` holds, else falls back to first-tool-only.
- **`NormalizeLLM`** — thin wrapper for callers that don't have a tool definition slice.
- **`allParallelSafe(calls, defs)`** — defensive default (unknown tool ⇒ NOT parallel-safe) so MCP/plugin tools never get raced unintentionally.

#### `internal/agent/agent.go` — `runParallelToolBatch`

- Worker pool capped by **`parallelBatchWorkerLimit = 16`** (`chan struct{}` semaphore).
- **PreTool hooks run SERIALLY** before the parallel fan-out. Most hooks aren't reentrant (shared log file, slow subprocess startup); 16 concurrent `powershell -Command Add-Content` invocations would file-lock against each other and time out at the 5 s hook timeout. Serial hooks + parallel tools keeps correctness while still capturing the I/O speedup that actually matters.
- Results collected by index → tool replies are stitched into history in the same order as the assistant message's `tool_calls`, satisfying the OpenAI contract.

#### `internal/agent/agent.go` — serial-fallback orphan cleanup

When the model emits a mixed batch (e.g. `[read, read, edit]`), `NormalizeLLM` keeps only the first call. The SSE parser already surfaced `tool_call_start` for every call, so the TUI would have stranded "running" blocks for the dropped extras. The agent emits synthetic `tool_call_completed` events with a `"skipped: …"` prefix for those orphans (see `ToolBlockSkipped` below).

#### `internal/llm/stream.go` — full tool-call surfacing

- Removed `primaryToolIdx` suppression that hid all but the first parallel `tool_call`. The stream now emits `tool_call_start`/`tool_call_delta` for every call so the TUI can render the full batch.
- Added **`reasoning_content` / `thinking_content` field parsing** for reasoning models (Qwen3, DeepSeek-R1 via LM Studio). When present, the parser wraps these chunks in synthetic `<think>…</think>` tags and feeds them into the same `MessageDelta` channel the TUI's `ReasoningSplitter` already understands — no new event kind needed.
- Added `ORCH_STREAM_DEBUG=path/to/file` env-flag SSE-tap for diagnosing what providers actually send.

#### Prompt updates (`internal/prompt/files/build*.txt`)

- All six build-mode prompts (`build.txt`, `build-local.txt`, `build-anthropic.txt`, `build-gemini.txt`, `build-gpt.txt`, `build-kimi.txt`) rewritten: **explicit encouragement of multi-call batches** for independent reads, with the rule "mutating (write/edit/bash) — strictly one per step, don't mix with other tools in the same batch". Replaces the old "не более одного tool call за шаг" wording that prevented the model from using the new parallel path.

### Changed — TUI: visual polish & UX (2026-05-12 / 2026-05-13)

#### Tool rendering (`ui/tui/view/`)

- **`renderBlockTool`** — removed `BackgroundSecondary` fill from completed exec/write tool panels. Now a plain left `┃` thick-border in `TextMuted` with no panel chrome. Body lines truncated to 20 (was 50).
- **`renderBlockTool`** — title is `<icon> <preview>`, not `# <preview>`.
- **`isBlockStyleTool(tb, streaming)`** — stricter: only `exec.run`, `fs.write`, `file.write_atomic` ever get block style; only when not streaming and not currently running. `read`/`list`/`search`/`symbols`/`glob` stay inline forever — their preview already carries a useful summary (`(N entries)`, `(N matches)`).
- **`renderToolGroup`** — collapsed view shows the **inline per-tool list** plus a compact muted footer (`└ N toolcalls · 7.0s · 1 failed · 2 skipped`); the "Build Task — query" header removed (the user's request panel above already shows the query).
- **`renderInlineTool`** — when `tb.Status == ToolBlockSkipped`, the label renders muted + `Faint` + `Strikethrough` so the user immediately recognizes "intentionally not executed" vs "errored".
- **`renderReasoning`** — wraps the "Thinking: …" block in a muted thick `┃` left-border (no fill) matching the user-message panel style.
- **Icon set unified** (`toolIcon`) — every tool's icon is a single-width single-rune from the same stylistic family. Glyph map: `→` read, `≡` list, `←` write, `✱` grep, `✦` glob (new), `◈` symbols, `$` exec, `▣` task, `•` unknown (was `⚙`). Added Claude-Code aliases to `toolKind` (`Read`, `Write`, `Edit`, `MultiEdit`, `Glob`, `LS`, `TodoWrite`).

#### Render cache (`ui/tui/view/render_cache.go`)

- **`renderCache.delete(key int64)`** — new method. `Chat.ExpandTurn` now invalidates the cached entry for that turn so Ctrl+T actually re-renders with the new expand state. Before this fix, Ctrl+T on a non-last completed assistant message was a visual no-op because the cache returned the old un-expanded render.
- **`Chat.SetMessages`** — expanded turns are never cached. Previously the cache would store an expanded render then return it after the user collapsed.

#### Streaming cursor removed (`view/message_assistant.go`)

- Dropped the `▋` cursor appended to the last assistant token while streaming. With `cursorBlink` toggling every ~500 ms, the cursor would alternately push text past the wrap point and back, causing visible screen jitter on every blink. The status bar's animated busy block now signals "agent is working" without reflowing chat content.

#### Text-from-non-final-step truncation (`app_rpc.go`)

- **`stepTextLen int`** field on `App` tracks the assistant text length at the start of each LLM step. On `EventStepDone(reason != "final")` the assistant message's `Text` is truncated back to `stepTextLen`, dropping pre-tool chatter and invalid-retry scratch output. Previously, when the model said "let me check that" then made a tool call then said "actually here's the answer", both texts ended up concatenated.
- Critical ordering fix: `stepTextLen` is updated in `EventStepDone` **after** truncation, not in `EventDone` (which fires earlier in the stream lifecycle).

#### Status bar redesign (`view/statusbar.go`)

- Two-row layout: top row is a blank gap (visual breathing room from the input box), bottom row is the actual status content.
- Left side reserved for live agent state — info parts (project, tokens, ctx) when idle, **OpenCode-style busy block** (`⠋ ▰▱▱ Read internal/agent/agent.go`) when `agentBusy`. Three accent glyphs cycle in/out with `spinFrame % 3` for a moving-block animation.
- Right side now hosts the context-sensitive hint ("Ctrl+K commands", "Esc cancel", …) — previously these took over the entire bar.
- **`SetActiveTool(name, path string)`** — `app_rpc.go` calls this on `EventToolCallStart`/`EventToolCallDelta` and clears it on `EventToolCallCompleted` / `EventStepDone`.

#### Chat area gutter (`app_view.go`)

- New `chatVerticalPad = 1` constant — 1-row blank gutter above and below the chat viewport so the first message doesn't kiss the top edge and the last message has breathing room from the input box. `layout()` subtracts `2*chatVerticalPad` from `chatHeight` so the input box doesn't get pushed off-screen.

#### Mouse wheel scroll (`ui/tui/app.go`)

- `tea.NewProgram` now starts with `tea.WithMouseCellMotion()`. The `MouseMsg` handler was already wired (`ScrollUp(3)` / `ScrollDown(3)` on wheel-up/down), it just never received events because the program wasn't subscribed to mouse motion.

### Added — Session `ToolBlockSkipped` status

- **`state.ToolBlockSkipped ToolBlockStatus = "skipped"`** — distinct from `Failed`. Set when `tool_call_completed.content` starts with `"skipped: "` (the prefix the agent uses for serial-fallback orphan extras). The TUI renders these muted + strikethrough so the user distinguishes "intentionally not executed" from "errored out".
- Removed the legacy auto-expand logic in `Session.UpdateToolBlock` that set `Expanded = true` on any tool whose result had ≤10 lines. With the new compact inline list as the default view, auto-expanding short results just put noise back in the chat.
- New helpers: `Session.TruncateAssistantText(n)`, `Session.AssistantTextLen()`, `Session.FindToolBlock(id)`, `renderCache.delete(key)`.

---

### Added — Sub-project G: Native LSP integration

#### New package `internal/lsp`

- **`framing.go`** — Content-Length framing: `ReadMessage(r *bufio.Reader) ([]byte, error)` and `WriteMessage(w io.Writer, body []byte) error`.
- **`protocol.go`** — LSP wire types: `Position`, `Range`, `Location`, `LocationLink`, `Diagnostic`, `TextEdit`, `WorkspaceEdit`, `MarkupContent`, `DiagnosticSeverity` (1–4 with `String()`).
- **`positions.go`** — Coordinate helpers: `PathToURI`, `URIToPath`, `ToolPosition` (1-based), `ToLSP`/`ToolPositionFrom` with UTF-16↔byte offset conversion.
- **`client.go`** — `Client` — persistent LSP subprocess client over stdio JSON-RPC 2.0. Methods: `Start`, `StartFromConn`, `Request`, `Notify`, `DidOpen/DidChange/DidClose`, `Close`. `readLoop` routes responses to pending channels and notifications to `notifyCh`.
- **`diagnostics.go`** — `DiagnosticsCache` — push-based diagnostics store. `Update/Get` for cached reads; `WaitForUpdate(ctx, uri)` channel-per-waiter pattern for async `publishDiagnostics` notifications.
- **`manager.go`** — `Manager` — multi-server routing by file extension. Inline `LSPConfig`/`LSPServerConfig` (no import cycle). Methods: `Definition`, `References`, `Hover`, `GetDiagnostics`, `Rename`, `SyncAndDiagnose`. Returns `ToolLocation`, `ToolDiagnostic`, `ProposedEdit` output types. Graceful degradation if no server handles the file.

#### New package `internal/lsp/lsptest`

- **`server.go`** — `Server` — in-process mock LSP server for tests. `New(conn)` / `NewConn()`, `SetHandler(method, fn)`, `PushDiagnostics(uri, diags)`. Auto-handles `initialize` (returns `utf-8` posEncoding), `shutdown`, `exit`.

#### New tools (5 LSP tools)

- **`lsp.definition`** — jump to definition via LSP.
- **`lsp.references`** — find all references via LSP.
- **`lsp.hover`** — hover documentation/type via LSP.
- **`lsp.diagnostics`** — get compiler/linter diagnostics for a file via LSP.
- **`lsp.rename`** — project-wide rename; returns `[]ProposedEdit` for the agent to apply via `fs.edit`/`fs.write`.

Added to: `listToolsBuild` (all 5), `listToolsPlan` / `listToolsExplore` (4, no rename), `listToolsGeneral` (all 5), `ListTools` base, `allToolDefsMap`. `ListToolsForChild` unchanged.

#### Auto-diagnostics on write/edit

- **`FSWriteResponse.Diagnostics []lsp.ToolDiagnostic`** — populated (when LSP is configured) by `SyncAndDiagnose` after every successful `fs.write`.
- **`FSEditResponse.Diagnostics []lsp.ToolDiagnostic`** — same for `fs.edit`. Gives the agent immediate error feedback without an extra tool call.

#### Config (`internal/config/config.go`)

- **`LSPServerConfig`** — `language`, `extensions`, `command`, `env`, `disabled`, `init_options`.
- **`LSPConfig`** — `enabled`, `servers`, `diagnostics_timeout_ms`.
- **`LSP LSPConfig`** field added to `ProjectConfig`.
- 5 LSP tool names added to `validAgentToolNames`.

#### Protocol bump

- `ToolsVersion` **4 → 5**: 5 new LSP tools + `diagnostics` field on write/edit responses.

#### Init template

- `orchestra init` now appends a commented-out `lsp:` block with gopls, typescript-language-server, pylsp, rust-analyzer examples.

### Added — Sub-project D: Custom agents in `.orchestra.yml`

#### Config (`internal/config/config.go`)

- **`AgentDefinition`** struct — `name`, `system_prompt`, `tools []string`, `model` per agent.
- **`agents:`** field on `ProjectConfig`; validated at `config.Load` time via `validateAgents()`:
  - empty name → error; collision with built-in mode name → error; duplicate names → error.
  - `tools: []` (explicit empty) → error ("omit to inherit"); `tools: null` → inherit full build toolset.
  - unknown tool name → error (guards against typos without import cycle).
- **`FindAgent(name) *AgentDefinition`** — O(n) lookup.
- **`IsBuiltInMode(name) bool`** — public predicate over the reserved-names map.

#### Tools registry (`internal/tools/registry.go`)

- **`ResolveToolNames(names []string) ([]llm.ToolDef, error)`** — maps short tool names to `llm.ToolDef` slices; returns error on unknown name.

#### Agent (`internal/agent/agent.go`)

- **`Options.SystemPromptOverride string`** — when non-empty, replaces the built-in mode system prompt before `.orchestra/system.txt` override.

#### Core (`internal/core/core.go`)

- **`AgentRunParams.Mode` / `SessionMessageParams.Mode`** — new optional field; enables custom agent by name on the JSON-RPC path.
- **`resolveCustomAgentOpts`** helper — centralises model override + tool resolution + MCP auto-append for both `AgentRun` and `SessionMessage`.
- Unknown `Mode` → `InvalidLLMOutput` protocol error.

#### CLI (`internal/cli/apply.go`)

- `--mode X` validation: unknown mode that is neither built-in nor in `agents:` → early error with helpful message.
- Direct mode: custom agent system_prompt + tool override + model override wired in.
- `--via-core` path: `Mode` forwarded in `agent.run` params.

#### Protocol bump

- `ProtocolVersion` **1 → 2**: `mode` field added to `agent.run` and `session.message` params (additive, `omitempty`).

#### Init template (`internal/cli/init.go`)

- `.orchestra.yml` generated by `orchestra init` now includes a commented-out advisor example in `agents:`.

### Added — Sub-project E: Permission ruleset per tool + glob

#### `permissions:` config block (`internal/config/config.go`, `internal/agent/permissions.go`)

- **`PermissionRule`** — ordered per-tool rule: `tool` (name or `*`), `pattern` (glob against subject), `action` (`allow` | `deny`).
- **`PermissionsConfig`** — list of rules, added as `permissions:` to `ProjectConfig`.
- **Subject table**: `bash` → command string; `webfetch` → URL; `write/edit/read/ls/grep/symbols` → file path; `glob` → glob pattern; `explore` → symbol name.
- **Glob semantics**: file-path subjects use `path.Match` (`*` does not cross `/`); non-path subjects (bash, webfetch, explore) use a simple wildcard where `*` matches any sequence including `/`.
- **First-match-wins** evaluation order (like iptables): rules are evaluated in order; the first matching rule's action wins.
- **`allow` → bypasses `--allow-exec` / `--allow-web` gates for that specific call only** — does not mutate `agent.Options`.
- **`deny` → always TOOL_DENIED**, regardless of `--allow-exec` / `--allow-web`.
- **No rules → no change** in behavior (existing consent gates are unchanged).
- Propagated through `apply.go`, `core.go`, `pipeline.go` (all three execution modes).

Example config:
```yaml
permissions:
  rules:
    - tool: bash
      pattern: "go test *"
      action: allow
    - tool: bash
      action: deny
    - tool: write
      pattern: "*.go"
      action: allow
    - tool: write
      action: deny
```

### Added — Sub-projects C+G: Compaction agent & Memory tool

#### Memory tool (`internal/tools/memory_write.go`)

- **`memory_write` tool** — агент записывает факты в `.orchestra/memory/agent.md` с ISO-timestamp. Файл создаётся автоматически; записи аппендятся.
- **`LoadProjectMemory` — аддитивный режим** — теперь читает ВСЕ три источника (ORCHESTRA.md + `.orchestra/memory/*.md` + `~/.orchestra/memory.md`) и конкатенирует их (ранее первый непустой выигрывал). Лимит поднят с 2 KB до 8 KB.
- `memory_write` добавлен в `ListTools`, `listToolsBuild`, `listToolsGeneral`.

#### Compaction agent (`internal/agent/compact.go`)

- **`historyBytes(history)`** — подсчёт размера истории в байтах (content + tool call args).
- **`compactHistory(ctx, userQuery, history)`** — вызывает LLM в режиме `ModeCompaction` (`compaction.txt` промпт, до 600 слов), сжимает историю в один `user`-message. Сбой — non-fatal (fallback на truncation).
- **`CompactThresholdPct int`** добавлен в `agent.Options` и `config.AgentConfig` (`compact_threshold_pct`). 0 = выключено, рекомендуется 70.
- Триггер срабатывает **только в начале итерации цикла** (история в консистентном состоянии — нет orphan tool_calls без tool_results).
- `CompactThresholdPct` пробрасывается через `cli/apply.go`, `internal/core/core.go`, `internal/pipeline/pipeline.go`.

### Added — Sub-project F: WebFetch tool

#### `webfetch` (`internal/tools/webfetch.go`)

- **`webfetch` tool** — HTTP GET любого `http://` или `https://` URL; возвращает `{url, title, content, truncated}`.
- **SSRF-защита** — custom `DialContext` резолвит DNS сам и блокирует private, loopback, link-local, multicast и unspecified адреса перед установкой соединения; raw IP-литералы проверяются напрямую.
- **HTML → текст** — `golang.org/x/net/html` парсит DOM; пропускаются `<script>`, `<style>`, `<noscript>`, `<iframe>`, `<svg>`, `<canvas>`; `<title>` извлекается отдельно.
- **Consent-гейт** — `--allow-web` CLI-флаг (зеркалит `--allow-exec`); дефолт `web.confirm: true`; отключается через `web.confirm: false` в `.orchestra.yml`.
- **Лимиты** — `web.fetch_timeout_s` (дефолт 30 с), `web.max_content_bytes` (дефолт 512 КБ); оба настраиваемы в конфиге.
- **`WebConfig`** добавлен в `internal/config/config.go`.
- **`AllowWeb bool`** добавлен в `agent.Options`; защитный check в агент-луп аналогичен bash/AllowExec.
- **`golang.org/x/net v0.53.0`** добавлен в go.mod.

### Added — Post Phase 9: Prompt pipeline, tool aliases, line numbers, forgiving resolver

#### Prompt pipeline (`internal/prompt/`)

- **go:embed промпты** — все промпты перенесены в `internal/prompt/files/*.txt` и встраиваются через `//go:embed files/*.txt`; никаких захардкоженных строк в Go-коде.
- **Маршрутизация по семейству модели** — `BuildSystemPromptForMode(mode, family)` ищет `{mode}-{family}.txt → {mode}.txt → build.txt`; `DetectPromptFamily(modelName)` автоматически определяет семейство.
- **Поддерживаемые семейства:** `anthropic`, `gpt`, `gemini`, `kimi` (Moonshot), `local` (qwen/llama/mistral/deepseek/phi).
- **7 режимов агента** — добавлены константы `ModeGeneral`, `ModeCompaction`, `ModeTitle`, `ModeSummary` к уже существующим `ModeBuild`, `ModePlan`, `ModeExplore`; промпты для каждого встроены через embed.
- **Max-steps reminder** — при достижении 2/3 лимита шагов в историю инжектируется синтетическое `role: assistant` сообщение из `max-steps.txt`, предотвращающее расходование последних шагов на исследование.
- **Lazy ORCHESTRA.md discovery** — `Runner.discoverInstructions` обходит от директории читаемого файла до `workspaceRoot` и инжектирует `<system-reminder>` в ответ `fs.read`; `seenInstructionDirs sync.Map` исключает повторы в рамках сессии.
- **Workspace system prompt override** — `.orchestra/system.txt` полностью заменяет встроенный системный промпт; `LoadSystemOverride(workspaceRoot)` читается в начале каждого шага.
- **Промпты разделены по файлам** — `system.go`, `family.go`, `reminders.go`, `snapshot.go`, `user.go` вместо монолитного `agent_prompt.go`.

#### Anthropic prompt caching (`internal/llm/anthropic.go`)

- Системный промпт оборачивается в `[]anthropicSystemBlock` с `cache_control: {type:"ephemeral"}`.
- Заголовок `anthropic-beta: prompt-caching-2024-07-31` добавлен к каждому запросу.
- Экономия: кэш-запись стоит ~25% дороже, но кэш-чтение экономит ~90% токенов; на сессии из 24 шагов это окупается со шага 2.

#### Tool aliases / short names (`internal/tools/registry.go`)

- Переименованы tool-имена, видимые LLM, в соответствии с конвенцией OpenCode:
  `fs.list` → `ls`, `fs.read` → `read`, `fs.glob` → `glob`, `fs.write` → `write`, `fs.edit` → `edit`, `search.text` → `grep`, `code.symbols` → `symbols`, `explore_codebase` → `explore`, `exec.run` → `bash`.
- `task.spawn/wait/cancel/result` → `task_spawn/wait/cancel/result`.
- `ToolsVersion` bumped `3 → 4`.

#### fs.read line numbers (`internal/tools/fs_read.go`)

- Каждая строка возвращается с префиксом `N: ` (例: `1: package main`).
- Модель видит номера строк для точных ссылок в `edit`; сами префиксы не входят в файл.
- `ToolsVersion` bumped `2 → 3`.

#### Forgiving resolver (`internal/resolver/`)

- При `StaleContent` резолвер делает второй проход с `lineTrimmedFind` (игнорирует хвостовые пробелы) перед тем как вернуть ошибку модели.
- **Pass 3 — IndentationFlexible**: третий проход `indentFlexibleFind` нормализует ведущие отступы (табы → 4 пробела), закрывая разрыв когда файл использует `\t`, а LLM прислал пробелы или наоборот.
- **Защита от ложных срабатываний**: совпадение принимается только если начинается на границе строки (`absJ==0 || normHay[absJ-1]=='\n'`), что не даёт 4-пробельной игле матчиться внутри 8-пробельной строки.
- Сокращает число «рибаундов» к LLM при незначительных расхождениях форматирования, сохраняя `file_hash`-гарантию.

#### Прочие изменения

- **`.gitignore`** — паттерн `orchestra` заменён на `/orchestra` и `/orchestra.exe`, чтобы директория `cmd/orchestra/` не исключалась из git.
- **`cmd/orchestra/main.go`** добавлен в tracking (ранее не коммитился из-за неверного gitignore).
- Удалены легаси-пакеты: `internal/applier`, `internal/parser`, пустые переходные пакеты, `testdata/`, `.eval_test/`.

### Added — Phase 9: Eval harness & provider support

- **Anthropic provider** (`internal/llm/anthropic.go`) — full OpenAI↔Anthropic message conversion; system prompt extracted separately; consecutive `role:tool` messages grouped into a single `tool_result` user message per API requirements; provider selected via `cfg.LLM.Provider = "anthropic"`.
- **Eval harness** (`tests/eval/`) — YAML task definitions, isolated temp workspaces, file-based checks (`file_exists`, `file_not_exists`, `file_contains`, `file_not_contains`), `LoadTasks()`, `Runner.RunTask()`.
- **`orchestra eval [tasks-dir]`** CLI command — runs eval tasks against the configured LLM, tab-formatted pass/fail report.
- Example eval tasks: `tests/eval/tasks/rename_func.yaml`, `add_func.yaml`.

### Added — Phase 8: MCP bridge

- **`internal/mcp/client.go`** — stdio subprocess JSON-RPC 2.0 MCP client; async pending map with channels; `Start()` → initialize handshake → `tools/list`.
- **`internal/mcp/manager.go`** — multi-server manager; `ListToolDefs()` prefixes tools as `mcp:<server>:<tool>`; `Call()` routes via server name; non-fatal per-server startup errors.
- `MCPCaller` interface on `tools.Runner`; routing via `strings.HasPrefix(name, "mcp:")` in `tools/call.go`.
- `MCPConfig` in config (servers with `command`, `env`, `disabled`).
- MCP tools appear as `ExtraTools` in `agent.Options`; `Core.mcpManager` started in `New()`, stopped in `Close()`.

### Added — Phase 7: Project memory

- **`internal/prompt/memory.go`** — `LoadProjectMemory(workspaceRoot, maxBytes)` reads from `ORCHESTRA.md` → `.orchestra/memory/*.md` (sorted, concatenated) → `~/.orchestra/memory.md`; caps at `maxBytes`; wraps in `<project_memory>` block.
- Memory automatically injected into the system prompt at each agent step.

### Added — Phase 6: Hooks

- **`internal/hooks/hooks.go`** — `Runner` executes pre/post tool call shell commands as subprocesses; `RunPreTool` non-zero exit → `TOOL_DENIED`; `RunPostTool` non-zero exit → warning log only (never blocks).
- `HooksConfig` in config (`enabled`, `pre_tool`, `post_tool`, `timeout_ms`).
- `HooksRunner` interface in `agent.Options`; nil-safe assignment in `core.go` prevents non-nil interface with nil pointer.
- Env vars set for hook scripts: `ORCH_TOOL_NAME`, `ORCH_TOOL_INPUT`, `ORCH_WORKSPACE_ROOT`.

### Added — Phase 5: Subagents

- **`internal/tasks/tasks.go`** — `TaskRunner` implements `agent.SubtaskRunner`; `Spawn()` starts child agent in a goroutine with optional timeout; `Wait()` blocks until done or times out; `Cancel()` cancels the child context.
- Child agents run with a read-only tool set (`ListToolsForChild`: `fs.list/read/glob`, `search.text`, `code.symbols`, `task.result`) and `SubtaskRunner: nil` to prevent recursive spawning.
- `task.result` tool — child calls it to return a string; parent agent intercepts and exits the loop with `Result.SubtaskResult`.
- `task.spawn / task.wait / task.cancel` tool definitions in `internal/tools/registry.go`.
- `ToolsVersion` bumped to `2` in `internal/protocol/version.go`.

### Added — New tools

- **`fs.write`** (`internal/tools/fs_write.go`) — atomic file write with optional backup.
- **`fs.edit`** (`internal/tools/fs_edit.go`) — search-and-replace within a file.
- **`fs.glob`** (`internal/tools/fs_glob.go`) — glob pattern file listing.
- **`todo.read / todo.write`** (`internal/tools/todo.go`) — in-process session task list (no filesystem).

### Added — Phase 3: Session API

- `internal/core/session/` — session state: history, todos, last result.
- Stateless `Agent.Run` — takes and returns `[]llm.Message` history slice; Core owns session.
- `OnEvent` callback for streaming events; `AgentLogger` writes `tool_call/tool_result` to `llm_log.jsonl`.

### Added — Phases 1–2: Streaming & grammar

- SSE stream parser, `StreamAccumulator` for tool call assembly across chunks.
- Grammar-constrained sampling (`ResponseFormat`); retry/circuit-breaker config; prompt families.

### Added — Phase 0: vNext core

- JSON-RPC 2.0 over stdio (`internal/jsonrpc`); `orchestra core --workspace-root .` server.
- `Core` + `RPCHandler` (`internal/core`): `initialize`, `agent.run`, `tool.call`, `core.health`.
- `internal/resolver` — `ExternalPatch` → `InternalOp` conversion; `file_hash` consistency checks.
- `internal/patches`, `internal/ops` — two-layer patch model.
- `orchestra daemon` — legacy v0.3 HTTP daemon (loopback-only, for backwards compatibility).

### Changed

- `ToolsVersion` → `2` (was `1`) due to new tool additions.
- Config: added `mcp`, `hooks`, `tasks` sections; `llm.provider` field.
- All disk writes go through atomic temp-file → fsync → rename.

### Tests

- New test packages for all vNext additions:
  - `internal/hooks` — pre/post subprocess, env vars, timeout, nil runner.
  - `internal/prompt` — all 3 memory sources, priority, truncation.
  - `internal/llm` — Anthropic conversion (system extraction, tool_result grouping, schema defaults).
  - `internal/mcp` — tool name parsing, nil-safe Manager, invalid routes.
  - `internal/tasks` — Spawn/Wait/Cancel lifecycle, mock LLM with `task.result`.
  - `tests/eval` — all check types, `LoadTasks`, `RunTask` with mock agent.

---

## [0.2.0] — Initial release

- v0.2 architecture: `pkg/cli`, `internal/context` builder, `internal/gitutil`, plan/apply pipeline.
- `orchestra apply`, `orchestra search`, `orchestra init`.
- OpenAI-compatible LLM client.
- Search with exclusion rules, diff-based apply with backup.
