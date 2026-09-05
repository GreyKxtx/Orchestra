# Orchestra

English | [Русский](README.ru.md)

**Local AI coding assistant** — an LLM reads your project, plans edits, and applies them safely.

Primary transport: **JSON-RPC 2.0 over stdio** (LSP-style); the CLI sits on top of it.

---

## Capabilities

| Phase | Feature | Status |
|------|------|--------|
| Core | JSON-RPC 2.0 stdio, agent loop, external/internal patches | ✅ |
| Streaming | SSE streaming, tool-call chunk accumulator | ✅ |
| Grammar | Structured output, retry/circuit-breaker, prompt families | ✅ |
| Session | Conversation history, todo list, `agent.run` over JSON-RPC | ✅ |
| Subagents | `task.spawn/wait/cancel`, child agents with read-only tools | ✅ |
| Hooks | Pre/post-tool shell hooks, `TOOL_DENIED` on nonzero exit | ✅ |
| Memory | `ORCHESTRA.md` → `.orchestra/memory/*.md` → `~/.orchestra/memory.md` | ✅ |
| MCP | JSON-RPC 2.0 stdio MCP client, multi-server manager | ✅ |
| Providers | Anthropic API + OpenAI-compatible providers (LM Studio, vLLM…) | ✅ |
| Eval | YAML task suites, isolated workspaces, `orchestra eval` | ✅ |
| Prompt Pipeline | go:embed .txt prompts, routing by model family (anthropic/gpt/gemini/kimi/local) | ✅ |
| Agent Modes | build, plan, explore, ask, debug, architecture, agent, orchestra, worker, … | ✅ |
| Prompt Caching | Anthropic `cache_control: ephemeral` — ~90% token savings from turn 2 onward | ✅ |
| Lazy Instructions | Automatic `ORCHESTRA.md` discovery on file reads | ✅ |
| Line Numbers | `fs.read` returns line-numbered content for precise edit anchors | ✅ |
| Forgiving Edit | Resolver: line-trimmed Pass 2 + indent-flexible Pass 3 (tab↔space) before `StaleContent` | ✅ |
| WebFetch | `webfetch` — HTTP GET with SSRF protection (private/loopback/link-local blocked), HTML→text | ✅ |
| Compaction | Auto history compaction at `compact_threshold_pct` of `MaxPromptBytes`; LLM summary, non-fatal fallback | ✅ |
| Memory tool | `memory_write` — the agent records facts to `.orchestra/memory/agent.md`; `LoadProjectMemory` is additive across all 3 sources | ✅ |
| Permission Rules | `permissions.rules` — per-tool allow/deny with glob patterns; first-match-wins; `allow` bypasses `--allow-exec/web` for a single call | ✅ |
| Parallel Tool Calls | `ParallelSafe`/`Mutating` flags on `llm.ToolDef`; read-only tools (`ls`/`read`/`glob`/`grep`/`symbols`/`explore`/`lsp.*`/`webfetch`) run concurrently in one batch through a 16-worker pool; mutating tools (`write`/`edit`/`bash`) run serially. Pre-tool hooks stay serial ahead of fan-out to avoid racing on shared state | ✅ |
| Reasoning Stream | Parses `delta.reasoning_content` / `delta.thinking_content` (Qwen3, DeepSeek-R1 via LM Studio); auto-wraps into `<think>…</think>` for `ReasoningSplitter`; SSE tap behind the `ORCH_STREAM_DEBUG` env flag | ✅ |
| TUI (Phase 0-5) | Bubbletea + lipgloss; inline tool list, OpenCode-style busy indicator in the status bar, mouse wheel scroll, "Thinking:" block with a `┃` border, render-cache invalidation on Ctrl+T, mode-aware accent colors | ✅ |
| Planner–Worker | `mode=orchestra` Lead + `subagent_type=worker`, WorkOrder JSON, `target_symbol` scoping, LSP E2E | ✅ |
| Orchestra Lead surface | Strict allowlist of **14 tools** (`listToolsOrchestra`); no edit/LSP/bash; Step-1 prompt **≤ 8k tokens** | ✅ |
| CKG v5 | Multi-hop explore (`depth`/`direction`), subgraph cap 1500 tokens, protocol **ToolsVersion 14** | ✅ |
| Learning stack | Dept lessons + playbooks with inject quotas; `lesson_promote` / `playbook_promote`; a single-agent turn that repeats the same anti-pattern 3× on one file offers a human `[y/n]` rule suggestion for `ORCHESTRA.md` | ✅ |
| LLM fail-fast | An unreachable endpoint (dial / refused / i/o timeout) aborts the turn — no false `prompt too large` compaction loop | ✅ |
| TUI Subagent Bar | Live child tasks (`child_started` / `child_queued` / `child_done`) | ✅ |
| Attachments / Vision | Protocol **v13**: images/SVG/PDF, staging in `.orchestra/attachments/`, TUI `/attach`, VS Code drag-drop | ✅ |
| VS Code extension | Webview chat + settings, LSP install modal, per-file diff review, workspace editor for previews | ✅ |

---

## Install

Prebuilt archives for windows-amd64, linux-amd64, darwin-arm64, and darwin-amd64 are published on GitHub Releases (tag `v*`), alongside a `.sha256` and `THIRD_PARTY_NOTICES.md`. Manual install: unpack and put `orchestra` on your `PATH`. Or use the install script, which detects your platform, downloads the right archive, and verifies its checksum:

```bash
# Linux / macOS
curl -fsSL https://raw.githubusercontent.com/GreyKxtx/Orchestra/master/scripts/install.sh | bash
```

```powershell
# Windows
irm https://raw.githubusercontent.com/GreyKxtx/Orchestra/master/scripts/install.ps1 | iex
```

Building from source needs a C compiler — CKG is built on tree-sitter via cgo (MinGW on Windows):

```bash
go install github.com/orchestra/orchestra/cmd/orchestra@latest
```

Check the install:

```bash
orchestra version
# orchestra v0.3.0 (a1b2c3d)
# protocol 13 · ops 1 · tools 14
```

Those three numbers are the `initialize` contract: if the TUI or the extension refuse to connect, the mismatch will be in one of them.

Each release also carries a Homebrew formula, a Scoop manifest, and a winget manifest set (`orchestra.rb`, `orchestra.json`, `orchestra-winget.zip` — generated by `scripts/gen-packaging.sh`). No tap/bucket repo exists yet, so install straight from the release asset:

```bash
# Homebrew (macOS / Linux)
brew install --formula https://github.com/GreyKxtx/Orchestra/releases/latest/download/orchestra.rb
```

```powershell
# Scoop (Windows)
scoop install https://github.com/GreyKxtx/Orchestra/releases/latest/download/orchestra.json
```

winget needs an unpacked manifest directory (`orchestra-winget.zip` → `winget install --manifest <folder>`); a public `winget install orchestra` needs a PR against `microsoft/winget-pkgs`, not done automatically by this repo.

## Quick start

```bash
# Build from the repo
go build -o orchestra ./cmd/orchestra

# Initialize a project
orchestra init

# Preview the plan (no files touched)
orchestra apply --plan-only "add logging to main.go"

# Dry-run apply (default — only shows the diff)
orchestra apply "add logging to main.go"

# Actually apply the changes (creates .orchestra.bak)
orchestra apply --apply "add logging to main.go"

# Export a unified .patch for review (disk untouched)
orchestra apply --output-patch "add logging to main.go"
orchestra apply --output-patch ./review.patch "…"

# Adaptive profiles: fast or precision
orchestra apply --profile fast "fix a typo in the README"
orchestra apply --profile precision "design package X"

# Allow command execution via exec.run
orchestra apply --apply --allow-exec "run go test and fix the failures"

# Allow fetching external URLs via webfetch
orchestra apply --allow-web "read the docs at https://pkg.go.dev/... and add an example"

# Via the subprocess core (JSON-RPC stdio, isolated)
orchestra apply --via-core "add a Sum function"

# Smoke-test the LLM connection
orchestra llm-ping

# Search the codebase
orchestra search "function main"

# Run eval tasks (needs a working LLM)
orchestra eval                          # tests/eval/tasks/ by default
orchestra eval path/to/tasks/           # a custom directory
```

### VS Code / Cursor extension

The client lives in `ui/vscode/` — webview chat, settings, attachments/vision (protocol **v13**). It needs a built `orchestra` binary on `PATH` or next to the repo.

```bash
go build -o orchestra ./cmd/orchestra
cd ui/vscode && npm ci && npm run compile
# F5 in VS Code, or package it: npm run package
```

Details: `ui/vscode/README.md`.

---

## Configuration (`.orchestra.yml`)

```yaml
project_root: .
exclude_dirs: [.git, node_modules, dist]

llm:
  provider: openai          # "openai" | "anthropic" | "azure"
  api_base: http://localhost:1234/v1   # LM Studio (Ollama: :11434/v1, vLLM: :8000/v1)
  api_key: ""
  model: qwen2.5-coder-7b-instruct
  max_tokens: 4096
  timeout_s: 120
  multimodal: true          # images in chat (TUI /attach, VS Code); needs a vision-capable model

# Fallback provider: when the endpoint is unreachable, the turn moves to
# providers.backup and stays there for the rest of the run (the switch is
# logged to llm_log.jsonl as provider.switch, and to usage.jsonl as its own
# line). Model errors (400/401) do not count as unreachable.
# llm:
#   fallback_provider: backup
# providers:
#   backup: {provider: openrouter, api_base: https://openrouter.ai/api/v1, model: ...}

# Extended thinking. Set per provider, so providers.lead and providers.worker
# can reason differently. Never sent to models the catalog marks as having no
# reasoning support (a 400 would follow otherwise).
# llm:
#   reasoning:
#     effort: high         # minimal | low | medium | high | max
#     budget_tokens: 16384 # optional; overrides effort where the provider takes a number

# Azure OpenAI: api_base is the resource endpoint; the key goes in the api-key header.
# llm:
#   provider: azure
#   api_base: https://my-resource.openai.azure.com
#   model: gpt-4o
#   azure:
#     deployment: prod-gpt4o   # defaults to model
#     api_version: 2024-10-21  # defaults to the current GA version

agent:
  profile: ""               # optional: fast | precision

apply:
  output: disk              # disk | patch
  patch_dir: .orchestra/patches

exec:
  confirm: true             # false = allow exec.run without --allow-exec

hooks:
  enabled: false
  pre_tool: ["sh", "-c", "echo pre"]  # nonzero exit = TOOL_DENIED
  post_tool: ["sh", "-c", "echo post"]
  timeout_ms: 5000

  # Matcher form: match is a regexp against the tool name (empty = all).
  # Both forms can be mixed; hook lists from ~/.orchestra/config.yml are not
  # replaced by the project's, they're merged (global list comes first).
  # pre_tool:
  #   - match: "write|edit"
  #     command: ["./scripts/gate.sh"]
  #     timeout_ms: 2000

  # Lifecycle events (same two forms):
  # session_start: ["./scripts/on-session.sh"]
  # user_prompt_submit: ["./scripts/check-freeze.sh"]   # can deny the turn or append context
  # pre_compact: ["./scripts/archive-history.sh"]
  # turn_end: ["./scripts/notify.sh"]

mcp:
  servers:
    # Local server — stdio subprocess
    - name: my-server
      command: ["node", "mcp-server.js"]
      env: {API_KEY: "..."}
      disabled: false

    # Remote server — Streamable HTTP
    - name: github
      url: https://api.example.com/mcp
      bearer_token_env: GITHUB_MCP_TOKEN   # token is read from the environment, not the config
      headers: {X-Tenant: acme}
      allowed_tools: ["repo_*"]
```

A server must set exactly one of `command` or `url` — otherwise the config fails to load. Plaintext `http://` to a non-loopback host is rejected (the token would go over the network in the clear); if that's an internal network and you want it anyway, set `allow_insecure_http: true`.

### Secrets: `.orchestra.local.yml`

Put API keys and personal overrides in `.orchestra.local.yml` next to `.orchestra.yml` (it's in `.gitignore`; `orchestra init` adds it there automatically). The overlay is deep-merged on top of the main config at load time; when settings are saved (TUI / VS Code), values that came from the overlay are **not** written back into the shared `.orchestra.yml`:

```yaml
# .orchestra.local.yml — not committed
llm:
  api_key: sk-or-...
providers:
  openrouter:
    api_key: sk-or-...   # only this leaf is masked; the rest of the provider's fields come from .orchestra.yml
```

### Global config: `~/.orchestra/config.yml`

Settings that are the same across every project — providers, keys, tiers, preferences — live in `~/.orchestra/config.yml`. The project's `.orchestra.yml` only needs to state what differs.

Layering order (later wins):

```
~/.orchestra/config.yml          user defaults
<project>/.orchestra.yml         shared, committed
<project>/.orchestra.local.yml   machine overrides and secrets
```

`project_root` from the global file is ignored — otherwise every project would point at the same directory. Keys set globally are **not** written back into the project file when settings are saved from the TUI / VS Code: `.orchestra.yml` is committed, and a key from your home directory can't leak into it that way.

### Hooks

A hook is just a process. It receives a JSON event on stdin and can answer with a decision on stdout:

```
stdin :  {"event":"pre_tool","tool":"write","input":{...},"session_id":"...","workspace_root":"..."}
stdout:  {"decision":"deny","reason":"edits are frozen on this branch until release"}
         {"decision":"modify","input":{"path":"safe.txt"}}
         {"decision":"allow","context":"repo is in a release freeze"}   # user_prompt_submit only
```

Everything is optional: a hook that reads no stdin and prints nothing behaves exactly as before — **a nonzero exit code still denies the call**, and output that isn't JSON is never treated as a decision (otherwise any hook with logging would become an accidental filter). `reason` is sent to the model instead of "pre-tool hook denied": a mechanism with no explanation gives the model nothing to fix, so it just calls the same tool again.

The same data also arrives as environment variables — `ORCH_TOOL_NAME`, `ORCH_TOOL_INPUT`, `ORCH_WORKSPACE_ROOT`, `ORCH_SESSION_ID`, `ORCH_HOOK_EVENT` — layered on top of the parent's environment (`PATH` included).

Events: `pre_tool`, `post_tool`, `session_start`, `user_prompt_submit` (can deny the turn or append context to it), `pre_compact` (the last moment before history gets compacted), `turn_end` (including for failed turns). For lifecycle hooks, `match` is checked against the event name.

### Project memory

Create `ORCHESTRA.md` at the project root — it's automatically injected into the agent's system prompt (max 2 KB). Alternatively: `.orchestra/memory/*.md` or `~/.orchestra/memory.md`. If the repo already has `AGENTS.md`, `CLAUDE.md`, or `.cursorrules`, Orchestra reads them as a fallback in that order when `ORCHESTRA.md` is absent.

Personal notes you don't want committed go in `ORCHESTRA.local.md` next to the main file (`orchestra init` adds it to `.gitignore` for you). Its content is appended to the team file, not swapped in for it.

Inside `ORCHESTRA.md` (or `ORCHESTRA.local.md`), `@import path/to/file.md` works — the path is resolved relative to the file that contains the `@import`, nesting depth ≤3, cycles are detected. That lets one root file pull in `docs/*.md` instead of duplicating their content. A broken import (wrong path, cycle, depth exceeded) doesn't take down the rest of the file — the `@import ...` line stays in place with an inline error marker.

In the TUI: `/memory` shows the memory layers and pinned facts; `/memory open` opens the real project-instructions file in `$EDITOR`; `/memory refresh` shows what actually went into the prompt on the last turn (bytes per layer against the budget) — read from the `memory.inject` event in `.orchestra/llm_log.jsonl`, the same log that already carries `memory.note` events.

### MCP server mode

`orchestra mcp serve` exposes Orchestra's own code-intelligence tools (`explore`, `semantic_search`, `symbols`, `repo_map`, `runtime_query`, `lsp.*`) as an MCP server, so any MCP-capable client (Claude Code, Claude Desktop, Cursor, …) can use them directly. stdio is the default transport; `--http` serves Streamable HTTP instead and always requires a token (`--mcp-token` or `$ORCH_MCP_TOKEN`).

```bash
orchestra mcp serve --workspace-root /path/to/project
```

---

## Architecture (key abstractions)

**Two patch layers, strictly separated:**

- **External Patches** (`internal/patches`) — the flexible LLM-facing format: `file.search_replace`, `file.unified_diff`, `file.write_atomic`. Each carries the `file_hash` of the version the LLM read.
- **Internal Ops** (`internal/ops`) — the deterministic on-disk write format: `file.replace_range`, `file.write_atomic`, `file.mkdir_all`. Coordinates are 0-based, end-exclusive. Every op carries `conditions.file_hash`.
- `internal/resolver` — the bridge: `ResolveExternalPatches` converts External → Internal by re-reading files and computing exact ranges.
- `internal/applier` — writes ops; with `apply.output=patch` / `--output-patch`, emits a unified diff without touching the workspace.

**Agent loop** (`internal/agent/agent.go`): system prompt + history → `llm.Complete` → `tool_call` (run it, append to history, continue) or `final` (resolve patches → apply). Recoverable errors (`StaleContent`, `AmbiguousMatch`) go back into history as compact hints. `fast`/`precision` profiles — see `docs/architecture/`.

**Three `apply` modes:**
1. `direct` — the agent runs in-process.
2. `--via-core` — spawns `orchestra core` as a subprocess, driven over JSON-RPC.
3. `--from-plan` — replays a saved `plan.json` with no LLM involved.

TUI pipeline audit: [docs/architecture/tui-pipeline.md](docs/architecture/tui-pipeline.md). Planner–Worker: [docs/architecture/planner-worker.md](docs/architecture/planner-worker.md).

---

## Tests

```bash
go vet ./...
go test ./...
go test -race ./...

# One package / one test
go test ./internal/agent -run TestAgent_Run -v
go test ./protocol/jsonrpc -race -count=10

# E2E against a real LLM (not part of CI)
$env:ORCH_E2E_LLM = "1"
go test ./tests/e2e_real_llm -v -count=1

# Planner–Worker E2E (mock, runs in CI)
go test ./tests/e2e_agent/... -run 'Orchestra|Worker|Ambiguous|Staging' -count=1
```

## TUI (console agent)

```bash
orchestra                  # interactive TUI (default)
orchestra --apply          # LIVE: writes to disk immediately
orchestra --apply --allow-exec
orchestra tui              # alias
```
 
---

## Documentation

- [Changelog](docs/CHANGELOG.md)
- [Protocol contract](docs/PROTOCOL.md)
- [Roadmap](docs/ROADMAP.md)
- [Agent modes](docs/modes.md) — authoritative for modes
- [Planner–Worker architecture](docs/architecture/planner-worker.md)
- [TUI pipeline](docs/architecture/tui-pipeline.md)
- [LSP auto-provision](docs/architecture/lsp-auto-provision.md)
- [Commands & CLI reference](docs/commands-and-modes.md)
- [Tools & commands status](docs/tools-status.md)
- [Package paths (authoritative map)](docs/architecture/paths.md)
- [Module layout & import rules](docs/architecture/modules.md)

---

## Requirements

- Go 1.22+
- LLM API: an OpenAI-compatible provider (LM Studio, vLLM, OpenAI, Anthropic…)

## License

MIT — see [LICENSE](LICENSE). Dependency licenses: [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
