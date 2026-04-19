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
go test -race -count=50 ./internal/jsonrpc ./internal/core   # stress

# Single package / single test
go test ./internal/agent -run TestAgent_Run -v
go test ./internal/jsonrpc -race -run TestServer -count=10

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
orchestra llm-ping                           # smoke-check the configured LLM
orchestra search "regex"                     # text search using project excludes
orchestra daemon --project-root .            # legacy v0.3 HTTP daemon (forced to 127.0.0.1)
```

`.orchestra.yml` (created by `init`) configures `project_root`, `exclude_dirs`, `llm.*`, `exec.*`, etc. — see `internal/config/config.go` for the full schema. `.orchestra/` is the per-project artifact dir (gitignored): `plan.json`, `diff.txt`, `last_run.jsonl`, `last_result.json`, `llm_log.jsonl`, plus debug discovery files.

## Architecture (the bits that need multiple files to understand)

**Two patch layers — keep them separate.** This is the central abstraction:

- **External Patches** (`internal/externalpatch`): the *flexible*, LLM-facing format. The agent only ever returns `final.patches` of type `file.search_replace`, `file.unified_diff`, or `file.write_atomic`. Each carries a `file_hash` (sha256) of the version the LLM read.
- **Internal Ops** (`internal/ops`): the *strict*, deterministic format that actually mutates disk — `file.replace_range`, `file.write_atomic`, `file.mkdir_all`. Coordinates are 0-based, end-exclusive. Every mutating op carries `conditions.file_hash` and the applier re-checks before writing.
- `internal/resolver` is the bridge: `ResolveExternalPatches` turns external patches into internal ops by re-reading files and computing exact ranges + anchors. The agent loop never emits internal ops directly.

**Agent loop** (`internal/agent/agent.go`, `Agent.Run`): system+user prompt → call `llm.Complete` with OpenAI-style tool defs (`internal/tools/registry.go`) → handle either `tool_call` (execute via `tools.Runner.Call`, append assistant+tool messages to history, loop) or `final` (resolve patches → `tools.FSApplyOps` with dry-run flag). Recoverable errors (`StaleContent`, `AmbiguousMatch`) feed compact hints back into history and the loop continues. Hard caps: `MaxSteps` (default 24), `MaxInvalidRetries` (3), `MaxFinalFailures` (6), `MaxDeniedToolRepeats` (2), `MaxToolErrorRepeats` (6), `LLMStepTimeout` (per step). `truncateMessages` keeps assistant+tool pairs together when shrinking history.

**Three execution modes for `apply`**, all defined in `internal/cli/apply.go::runApply`:
1. `direct` — agent runs in-process against the local `tools.Runner`.
2. `via-core` (`--via-core`) — spawns `orchestra core` as a subprocess and drives it via `internal/jsonrpc` (`initialize` → `agent.run`). Use this when isolation matters; real-LLM E2E tests use it.
3. `from-plan` (`--from-plan`) — no LLM call; loads a saved `plan.json` and replays its `ops` through the same applier. Critical for deterministic re-application and for the stale-content E2E tests.

**Core / RPC** (`internal/core`, `internal/jsonrpc`, `internal/protocol`): `Core` owns `cfg`, `llmClient`, `tools.Runner`, `schema.Validator`. `RPCHandler` exposes `core.health`, `initialize`, `agent.run`, `tool.call`. Pre-`initialize`, only `core.health` and `initialize` are allowed (others return `NotInitialized`). `initialize` is idempotent for the same params and hard-fails on mismatched `protocol_version` / `ops_version` / `tools_version` / `project_root` / `project_id`. Versions live in `internal/protocol/version.go` — bump them together when the contract changes and update `docs/PROTOCOL.md`.

**Tools** (`internal/tools`): `fs.list`, `fs.read`, `search.text`, `code.symbols`, `exec.run`. `ListTools(allowExec)` is the single source of truth for what the model sees; `exec.run` is *only* added when `allowExec=true`. `Runner` enforces `project_root` containment and `exec.run` timeout/output caps.

**Safety invariants** (from `.cursor/rules/projectrules.mdc` — treat as binding):
- All disk writes are atomic: temp file → fsync → rename. Use `daemon.AtomicWriteFile` for artifacts; the ops applier handles file content.
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
