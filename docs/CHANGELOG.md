# Changelog

All notable changes to Orchestra are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

---

## [Unreleased] — vNext

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
