# Package paths — authoritative map (post-modularization)

Use this when navigating the repo or writing docs. **Do not** reference deleted `internal/{protocol,llm,ops,applier,…}` paths.

## Go modules (`go.work`)

| Module | Import path | Contents |
|--------|-------------|----------|
| Root | `github.com/orchestra/orchestra` | `cmd/`, `internal/*`, `ui/tui`, tests |
| Protocol | `github.com/orchestra/orchestra/protocol` | `protocol/`, `protocol/jsonrpc`, `protocol/schema` |
| Patch | `github.com/orchestra/orchestra/patch` | `patch/ops`, `patches`, `resolver`, `applier`, `fsutil`, `cache`, `relpath` |
| LLM | `github.com/orchestra/orchestra/llm` | clients, streaming, `lmstudio/` |

```bash
go work sync   # after clone / go.mod changes
go vet ./... ./protocol/... ./patch/... ./llm/...
```

## Root `internal/` — by layer

| Layer | Path | Role |
|-------|------|------|
| CLI | `internal/cli/` | cobra commands (`apply`, `core`, `tui`, `eval`, …) |
| Core | `internal/core/` | JSON-RPC, sessions, agent.run, workflow |
| Agent | `internal/agent/` | LLM loop (`agent_run`, `agent_step`, `tool_parallel`, …) |
| Tools | `internal/tools/` | registry + Runner + subpackages (see below) |
| UI DTOs | `internal/uimodel/` | chat Message/Segment (no `ui/*` import) |
| Sessions | `internal/sessionfile/`, `internal/sessionstore/` | on-disk v4 schema + helpers |
| Integrations | `internal/lsp/`, `internal/mcp/`, `internal/ckg/` | LSP, MCP, code graph |
| Config | `internal/config/` | `.orchestra.yml` schema |

## `internal/tools/` subpackages

| Subpackage | Tools / role |
|------------|----------------|
| `fs/` | read, write, edit, glob, list, grep, staging, diff.preview |
| `exec/` | bash / exec.run, background shell |
| `git/` | status, log, diff, commit, gh PR/issue |
| `web/` | webfetch, websearch, browser (Playwright) |
| `toolslsp/` | lsp.* tools |
| `nav/` | explore, symbols, semantic_search, repo_map, CKG admin |
| `session/` | todo, memory, question, runtime_query |
| `task/` | task_spawn/wait, plan_enter/exit, skill_invoke |
| Root | `registry.go`, `runner.go`, `call.go`, `*_delegate.go` |

## Clients (never imported by core)

| Client | Path |
|--------|------|
| TUI | `ui/tui/` → spawns `orchestra core`, `session.*` |
| VS Code | `ui/vscode/` → TypeScript webview + `CoreSession` |
| Desktop | `ui/desktop/` — placeholder only |

## Removed (do not reintroduce)

- `internal/daemon/` — v0.3 HTTP daemon (removed)
- `orchestra daemon` CLI
- `internal/{protocol,jsonrpc,schema,ops,applier,patches,resolver,fsutil,cache,relpath,llm}/` → use sub-modules above
- `orchestra chat` REPL — use TUI / VS Code

CI enforces: `go test ./tests/importrules/...`

## Related docs

- Layer rules: `docs/architecture/modules.md`
- Wire contract: `docs/PROTOCOL.md`
- Streaming: `docs/architecture/streaming.md`
- TUI pipeline: `docs/architecture/tui-pipeline.md`
