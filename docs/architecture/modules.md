# Module layout & dependency rules

Orchestra is a **monorepo** with one Go module (`github.com/orchestra/orchestra`) today. Logical layers are enforced by import direction; future phases may extract layers into separate Go modules with `go.work`.

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
    ├── internal/llm, mcp, lsp, ckg    providers & integrations  →  **llm/ sub-module** (+ lmstudio)
    │
internal/patch stack   patches → resolver → applier (+ ops, fsutil, cache)
    │
internal/protocol      wire DTOs, version constants  →  **moved to `protocol/` sub-module**
internal/jsonrpc       stdio/HTTP transport           →  **`protocol/jsonrpc`**

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
| `internal/protocol`, `jsonrpc`, `schema` | stdlib, each other | `agent`, `tools`, `core`, `ui/*` |
| patch stack (`ops`, `patches`, `resolver`, `applier`, `fsutil`) | protocol, stdlib | `agent`, `llm`, `ui/*` |
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

## Phase 5+

## In-repo splits (before multi-module)

Prefer **subpackages** over new modules when coupling is high:

| Package | Subdirs |
|---------|---------|
| `internal/tools` | `exec/`, `git/`, `web/`, `toolslsp/`, `toolpath/`, `toolschema/` — registry stays root; **`fs/` pending** |
| `internal/core` | already split: `runtime_*.go`, `session/` |
| `internal/agent` | already has `digest/`, `guard/`, `history/`, … |

Do **not** extract `internal/daemon` — legacy; deprecate instead.

## Verification

```bash
# No internal→ui imports (except ui/tui itself)
go list -f '{{.ImportPath}} {{.Imports}}' ./internal/... | findstr ui/tui
# Should return nothing outside allowed paths

go vet ./...
go test ./internal/uimodel/... ./internal/sessionstore/... ./ui/tui/...
```
