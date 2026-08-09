# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Orchestra is a Go CLI ("local AI coding assistant core") that lets an LLM read a project, plan edits, and apply them safely. The primary protocol surface is **JSON-RPC 2.0 over stdio** (LSP-style framing); a CLI wraps it. See `docs/PROTOCOL.md` for the wire contract and `.cursor/rules/projectrules.mdc` for hard architectural constraints.

The repo is mid-transition to "vNext" — git status shows large deletions of v0.2 packages (`pkg/cli/...`, `internal/context/...`, `internal/gitutil/...`) and additions of vNext packages (`internal/core`, `internal/agent`, `internal/protocol`, `internal/jsonrpc`, etc.). When something feels duplicated (e.g. `internal/git` vs the now-empty `internal/gitutil`, or `pkg/cli/*` references in old docs), the new path under `internal/` is authoritative.

## Build & test

```bash
# Build
go build -o orchestra ./cmd/orchestra        # produces orchestra(.exe)

# Vet + tests (matches CI: ubuntu + windows)
go vet ./...
go test ./...
go test -race ./...                          # Linux/macOS or Windows w/ cgo
go test -race -count=50 ./protocol/jsonrpc ./internal/core   # stress

# Single package / single test
go test ./internal/agent -run TestAgent_Run -v
go test ./protocol/jsonrpc -race -run TestServer -count=10

# Real-LLM E2E (NOT in CI — gated by env var)
$env:ORCH_E2E_LLM = "1"                      # PowerShell
go test ./tests/e2e_real_llm -v -run TestRealLLMMinimalFlow -count=1
# Optional overrides: ORCH_E2E_LLM_API_BASE, ORCH_E2E_LLM_API_KEY, ORCH_E2E_LLM_MODEL
```

There is no linter beyond `go vet`. CI (`.github/workflows/ci.yml`) runs vet + tests on Linux (with `-race`) and Windows (without `-race`) — keep both green.

## Runtime / CLI

```bash
orchestra init                               # writes .orchestra.yml in cwd
orchestra core --workspace-root .            # JSON-RPC 2.0 over stdio
orchestra core --workspace-root . --http     # debug-only HTTP (loopback + token)
orchestra apply "..."                        # dry-run by default
orchestra apply --apply "..."                # write changes (creates .orchestra.bak)
orchestra apply --via-core "..."             # run agent inside subprocess core
orchestra apply --plan-only "..."            # plan only, no LLM-driven edits
orchestra apply --from-plan plan.json        # replay a saved plan with no LLM call
orchestra apply --apply --allow-exec "..."   # allow exec.run (off by default)
orchestra apply --output-patch [path] "..."  # export unified .patch; do not write workspace
orchestra apply --profile fast|precision "..." # adaptive execution presets
orchestra apply --skill <name> "..."         # run apply with a file-based skill
orchestra apply --provider <name> "..."      # override LLM provider for the run
orchestra llm-ping                           # smoke-check the configured LLM
orchestra search "regex"                     # text search using project excludes
orchestra skills list|show <name>            # introspect file-based skills
orchestra mcp list-tools                     # list tools from configured MCP servers
orchestra daemon --project-root .            # legacy v0.3 HTTP daemon (forced to 127.0.0.1)
```

`.orchestra.yml` (created by `init`) configures `project_root`, `exclude_dirs`, `llm.*`, `agent.profile`, `apply.output` / `apply.patch_dir`, `exec.*`, etc. — see `internal/config/config.go` for the full schema. `.orchestra/` is the per-project artifact dir (gitignored): `plan.json`, `diff.txt`, `last_run.jsonl`, `last_result.json`, `llm_log.jsonl`, plus debug discovery files. TUI pipeline audit: `docs/architecture/tui-pipeline.md`.

## Architecture (the bits that need multiple files to understand)

**Two patch layers — keep them separate.** This is the central abstraction:

- **External Patches** (`internal/patches`): the *flexible*, LLM-facing format. The agent only ever returns `final.patches` of type `file.search_replace`, `file.unified_diff`, or `file.write_atomic`. Each carries a `file_hash` (sha256) of the version the LLM read.
- **Internal Ops** (`internal/ops`): the *strict*, deterministic format that actually mutates disk — `file.replace_range`, `file.write_atomic`, `file.mkdir_all`. Coordinates are 0-based, end-exclusive. Every mutating op carries `conditions.file_hash` and the applier (`internal/applier`) re-checks before writing.
- `internal/resolver` is the bridge: `ResolveExternalPatches` turns external patches into internal ops by re-reading files and locating the search string via three-pass matching (exact → line-trimmed → indent-flexible). On ambiguity or no match it returns `AmbiguousMatch` / `StaleContent` errors that the agent loop feeds back as hints. The agent loop never emits internal ops directly.

**Agent loop** (`internal/agent/agent.go`, `Agent.Run`): system+user prompt → call `llm.Complete` with OpenAI-style tool defs (`internal/tools/registry.go`) → handle either `tool_call` (execute via `tools.Runner.Call`, append assistant+tool messages to history, loop) or `final` (resolve patches → `tools.FSApplyOps` with dry-run flag). Recoverable errors (`StaleContent`, `AmbiguousMatch`) feed compact hints back into history and the loop continues. Hard caps: `MaxSteps` (default 24), `MaxInvalidRetries` (3), `MaxFinalFailures` (6), `MaxDeniedToolRepeats` (2), `MaxToolErrorRepeats` (6), `LLMStepTimeout` (per step). `truncateMessages` keeps assistant+tool pairs together when shrinking history.

**Three execution modes for `apply`**, all defined in `internal/cli/apply.go::runApply`:
1. `direct` — agent runs in-process against the local `tools.Runner`.
2. `via-core` (`--via-core`) — spawns `orchestra core` as a subprocess and drives it via `protocol/jsonrpc` (`initialize` → `agent.run`). Use this when isolation matters; real-LLM E2E tests use it.
3. `from-plan` (`--from-plan`) — no LLM call; loads a saved `plan.json` and replays its `ops` through the same applier. Critical for deterministic re-application and for the stale-content E2E tests.

**Core / RPC** (`internal/core`, `protocol/jsonrpc`, `protocol`): `Core` owns `cfg`, `llmClient`, `tools.Runner`, `schema.Validator`. `RPCHandler` exposes `core.health`, `initialize`, `agent.run`, `tool.call`. Pre-`initialize`, only `core.health` and `initialize` are allowed (others return `NotInitialized`). `initialize` is idempotent for the same params and hard-fails on mismatched `protocol_version` / `ops_version` / `tools_version` / `project_root` / `project_id`. Versions live in `protocol/version.go` — bump them together when the contract changes and update `docs/PROTOCOL.md`.

**Tools** (`internal/tools`): the model-facing surface is large and grows by feature flag. `ListTools(allowExec, allowWeb, allowBrowser)` in `registry.go` is the single source of truth; per-mode variants (`ListToolsForMode`, `ListToolsWithSubtasks`, `ListToolsForChild`, …) layer on top. Full per-tool status: `docs/tools-status.md`. Headline groups:

- **Filesystem**: `ls/read/glob/write/edit/fs.delete/fs.rename/diff.preview`. `write`/`edit` write to a per-run **staging overlay** in dry-run mode (`internal/tools/staging.go`) — disk is only touched when `--apply` is set or `agent.run apply: true`.
- **Search/nav**: `grep` (auto-fallback to `rg`), `symbols`, `explore` (CKG: package / type / symbol level).
- **LSP**: `lsp.definition/references/hover/diagnostics/rename`; diagnostics auto-injected into agent history after successful `edit`/`write`.
- **Exec / web**: `bash` (`exec.run`, gated by `--allow-exec` or `exec.confirm:false`), `webfetch`, `websearch` (`--allow-web`).
- **Git / GitHub**: read-only `git.status/log/diff`, mutating `git.commit/branch/checkout/push` (allowExec-gated), `gh.pr.list/view/create`, `gh.issue.list/view`.
- **Browser**: 10 Playwright-MCP tools registered only under `--allow-browser`.
- **Subagents/session**: `task_spawn/wait/cancel/result`, `todowrite/todoread`, `memory_write`, `plan_enter/exit`, `question`, `runtime_query`.
- **Skills** (`internal/skills/`): file-based agent bundles in `~/.orchestra/skills/` (user-global) and `<project>/.orchestra/skills/` (project overrides user). Invokable two ways — CLI `apply --skill <name>` (whole run uses the skill), or in-process `skill_invoke{skill, task}` (model delegates a subtask synchronously). When any skill is discovered, the agent advertises them in a `<available_skills>` block and gets the `skill_invoke` tool. `$ARGUMENTS` in a skill body is substituted with the user query / task arg. See `docs/skills.md`.
- **MCP**: external server tools appear as `mcp:<server>:<tool>`; `orchestra mcp list-tools` introspects.

`Runner` enforces `project_root` containment, `exec.run` timeout/output caps, and per-tool permission rules from `cfg.Permissions.Rules` (allow/deny by tool name + glob path).

**Safety invariants** (from `.cursor/rules/projectrules.mdc` — treat as binding):
- All disk writes are atomic: temp file → fsync → rename. Use `fsutil.AtomicWriteFile` for artifacts; the ops applier handles file content.
- Never read or write outside `project_root`. Resolve symlinks/junctions and fail closed on escape.
- `file.replace_range` requires `before_anchor` + `after_anchor`; mismatched/ambiguous → fail with diagnostic, not best-effort.
- `apply` is dry-run unless `--apply` (or `agent.run` `apply: true`); on write, backup to `*.orchestra.bak` by default.
- `exec.run` requires explicit consent — `--allow-exec` on the CLI, or `exec.confirm: false` in config; the JSON-RPC handler also blocks it when `cfg.Exec.Confirm` is true.
- Top-level JSON-RPC arrays (batch) are *not* supported — return `-32600` with `id: null`. `id: null` is a request, not a notification.

## Test seams worth knowing

- `internal/cli.SetTestClient(llm.Client)` / `ResetTestClient()` — DI hook so tests can inject a mock LLM into `apply` without spinning up a real provider.
- `tests/integration/mock_llm` — scripted LLM fixtures used by integration tests; no network.
- `tests/e2e_real_llm` is gated by `ORCH_E2E_LLM=1` and expects a built `orchestra` binary on PATH or in repo root. It uses `--via-core` to exercise the JSON-RPC subprocess path.
- Per `.cursor/rules`: no network in unit tests, no real-LLM in benchmarks.

## Conventions to preserve

- Idiomatic Go, no panics for expected failures, errors wrap with `fmt.Errorf("...: %w", err)`. Concurrency uses `context.Context` + `sync.Mutex/RWMutex`; goroutines must have a stop path.
- Don't reintroduce the v0.2 patterns being deleted: `pkg/cli`, `internal/context` builder, the old `daemon.json`/`cache.json` discovery dance. The HTTP daemon (`orchestra daemon`) and the HTTP debug endpoint on `core --http` are kept but are debug/legacy; the supported transport is stdio JSON-RPC.
- Public CLI flags and the JSON-RPC method names/params are part of the contract. Bump `ProtocolVersion` / `OpsVersion` / `ToolsVersion` rather than silently changing them.
