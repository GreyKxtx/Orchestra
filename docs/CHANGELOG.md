# Changelog

All notable changes to Orchestra are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

---

## [Unreleased] — vNext

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
