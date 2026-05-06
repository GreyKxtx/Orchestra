# Changelog

All notable changes to Orchestra are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

---

## [Unreleased] — vNext

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
- `internal/externalpatch`, `internal/ops` — two-layer patch model.
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
