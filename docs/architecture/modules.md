# Module layout & dependency rules

Orchestra is a **monorepo** with a root module plus extracted sub-modules (`protocol/`, `patch/`, `llm/`) wired via **`go.work`**. Logical layers are enforced by import direction and CI (`tests/importrules`).

## Layer diagram

```
cmd/orchestra          CLI entry (cobra)
    │
internal/cli           subcommands
    │
internal/core          JSON-RPC orchestrator (sessions, agent.run, tools runner)
    │
    ├── internal/agent (+ subpackages)   LLM loop
    ├── internal/tools                   tool registry + execution
    ├── llm/ (sub-module), mcp, lsp, ckg    providers & integrations
    │
patch/ (sub-module)    patches → resolver → applier (+ ops, fsutil, cache)
    │
protocol/ (sub-module) wire DTOs, version, jsonrpc, schema

Clients (must not be imported by core):
    ui/tui               Bubble Tea client (Go)
    ui/vscode            VS Code extension (TypeScript / npm)

Shared UI DTOs (client-neutral):
    internal/uimodel     chat Message, Segment, ToolBlock, …
    internal/sessionfile on-disk session v4 schema
    internal/sessionstore persistence helpers (uses uimodel, not ui/*)
```

## Import rules (binding)

| Layer | May import | Must NOT import |
|-------|------------|-----------------|
| `protocol/`, `protocol/jsonrpc`, `protocol/schema` | stdlib, each other | `agent`, `tools`, `core`, `ui/*` |
| `patch/*` (ops, patches, resolver, applier, fsutil) | protocol, stdlib | `agent`, `llm`, `ui/*` |
| `internal/uimodel`, `sessionfile` | stdlib | `ui/*`, `agent`, `tools` |
| `internal/tools` | patch, llm, lsp, ckg, config, … | `ui/*`, `cli` |
| `internal/agent` | tools, llm, prompt, … | `ui/*`, `cli`, `core` |
| `internal/core` | agent, tools, sessionfile, uimodel, … | `ui/*` (except none — **zero** ui imports) |
| `internal/sessionstore` | uimodel, sessionfile | **`ui/*`** |
| `ui/tui` | protocol, uimodel (via state aliases), rpcclient | should avoid `internal/agent` directly |
| `ui/vscode` | — (TypeScript); DTOs from `docs/PROTOCOL.md` | Go internals |

**Hard rule:** `internal/*` packages below `core` must never import `ui/*`. Chat message types live in `internal/uimodel`.

## Phase 0 (done)

- [x] Extract `internal/uimodel` — neutral chat DTOs + `ToSessionfile` / `FromSessionfile`
- [x] `internal/sessionstore` uses `uimodel` (no `ui/tui/state` import)
- [x] `ui/tui/state` re-exports `uimodel` types; TUI-specific `Session` streaming helpers stay in TUI

## Phase 1 (done)

- [x] Sub-module `protocol/` — `github.com/orchestra/orchestra/protocol` (`protocol`, `jsonrpc`, `schema`)
- [x] Root `go.work` + `replace` in main `go.mod`
- [x] CI: `go work sync` before vet/test

## Phase 2 (done)

- [x] Sub-module `patch/` — `ops`, `patches`, `resolver`, `applier`, `fsutil`, `cache`, `relpath`
- [x] Depends on `protocol/`; `go.work` includes `./patch`

## Phase 3 (done)

- [x] Sub-module `llm/` — clients, streaming, catalog, `LLMConfig` types, `lmstudio/`
- [x] `internal/config` aliases `LLMConfig` → `llm.LLMConfig`; `ProjectConfig.LLMRegistry()` for provider lookup
- [x] `go.work` includes `./llm`

## Phase 4 (done)

- [x] In-repo split `internal/tools` into subpackages: `exec/`, `git/`, `web/`, `toolslsp/`, `fs/` (+ existing `toolpath/`, `toolschema/`)
- [x] Root `registry.go` keeps `ListTools*` / parallel flags; tool defs for exec/git/web/browser/LSP/FS live in subpackages (`Tool*` + `toolschema.MustSchema`)
- [x] Root `Runner` delegates via `*_delegate.go`; public API preserved with type aliases in `aliases.go` / `web_delegate.go`
- [x] `fs/` — write/edit/staging overlay, list/read/glob/grep, delete/rename, diff.preview, ast_rename; `Client` + `Hooks` for LSP/CKG/memory integration without importing parent `tools`

## Phase 4b (done)

- [x] **`internal/tools/nav/`** — `explore`, `symbols`, `semantic_search`, `repo_map`, CKG admin (`CKGIndexStatus`, `RebuildCKG`, `RunCKGEmbed`); `Client` + CKG/LSP snapshots
- [x] **`internal/tools/session/`** — `todowrite/todoread` types + `ValidateTodos`, `memory_*`, `runtime_query`, `question`; `Client` with session/memory/CKG callbacks
- [x] Root **`nav_delegate.go`**, **`session_delegate.go`**; **`aliases.go`** re-exports nav/session types + `ToolSemanticSearch` / `ToolRepoMap`
- [x] Monolith `registry.go` trimmed — nav/session tool defs removed; `ListTools*` calls `nav.Tool*` / `session.Tool*`

## Phase 4c (done)

- [x] **`internal/tools/task/`** — `task_spawn/wait/cancel/result`, unified `task`, `plan_enter/exit`, `ToolSkillInvoke`
- [x] Root **`registry.go`** delegates task/plan/skill defs to `task.*`; removed `path_shim.go`

## Phase 5 — governance (done)

- [x] **`tests/importrules/`** — CI gate: no `ui/*` imports in core layers; agent must not import `cli`; sessionstore must not import `ui/*`
- [x] **`.github/workflows/ci.yml`** — `go test ./tests/importrules/...` on Linux + Windows

## Phase 5a — core split (done)

- [x] **`internal/core/session_rpc.go`** — Session JSON-RPC surface extracted from `core.go`
- [x] **`internal/core/core_agent.go`** — `agent.run`, `tool.call`, usage helpers, MCP/custom-agent tool defs

## Phase 5b — agent split (done)

- [x] **`internal/agent/tool_parallel.go`** — parallel tool batch + JSON error/denial helpers
- [x] **`internal/agent/agent_run.go`** — main `Run` loop
- [x] **`internal/agent/agent_step.go`** — `nextStep`, streaming, token estimate helpers
- [x] **`internal/agent/agent_prompt.go`** — system prompt + tool def assembly

## Phase 6 — tools cleanup (done)

- [x] **`call.go` split** — `call.go` (entry + MCP routing), `call_dispatch.go` (table-driven dispatch), `call_decode.go` (JSON helpers)
- [x] **Tests colocated with subpackages** — `exec/`, `git/`, `web/` (incl. browser), `nav/`, `session/`, `task/`, `toolslsp/`, `fs/`; root keeps only `Runner`-level integration tests
- [x] **Removed `orchestra daemon` CLI** and **`internal/daemon`** (v0.3 HTTP); benchmarks use direct search only

## Phase 6+

Prefer **subpackages** over new modules when coupling is high:

| Package | Subdirs |
|---------|---------|
| `internal/tools` | `exec/`, `git/`, `web/`, `toolslsp/`, `fs/`, `nav/`, `session/`, `task/`, `toolpath/`, `toolschema/` — registry + `Runner` stay root |
| `internal/core` | already split: `runtime_*.go`, `session/` |
| `internal/agent` | already has `digest/`, `guard/`, `history/`, … |

Do **not** reintroduce `internal/daemon` or `orchestra daemon`. See `docs/architecture/paths.md`.

## Verification

```bash
# Layer import rules (also enforced in CI via tests/importrules)
go test ./tests/importrules/... -count=1

go vet ./...
go test ./internal/uimodel/... ./internal/sessionstore/... ./ui/tui/...
```
