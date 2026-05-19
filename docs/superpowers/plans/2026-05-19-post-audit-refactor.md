# Post-Audit Refactor Plan — 2026-05-19

> **Scope:** Findings from the C-phase deep audit of `internal/agent`, `internal/resolver`, `internal/lsp`, `internal/mcp` (4 parallel reviewers, commit `9805f0e`). Roll-up of bugs and structural risks the previous A/B audits did not cover.

## Methodology

Four parallel reviewers, one per subsystem, with these guardrails: real bugs only (no stylistic notes), `file:line` reference required, severity rubric `critical/high/medium/low`. Findings consolidated below; nothing reflects "nice-to-have" until the **Low** section.

Severity meaning in this doc:

- **CRITICAL** — silent data corruption, deadlock, process kill, or contract-break that breaks the next user request. Must fix before next release tag.
- **HIGH** — observable user-facing regression OR security/safety hole. Fix in current sprint.
- **MEDIUM** — degrades reliability, observability, or LLM efficiency; not user-blocking. Backlog.
- **LOW** — cosmetic, narrow edge case, or pre-existing limitation worth noting.

Effort tags: **S** (≤ 1h, single file), **M** (1–4h, 1–3 files), **L** (½–1 day, cross-package or design work).

## Cross-cutting themes (group fixes here for one PR)

Several findings cluster on the same underlying gap. Fix them as one work-item to avoid touching the same file twice:

1. **Subprocess startup timeouts** — LSP `client.go:100` and MCP `client.go:174,182` both call `initialize` with the caller's ctx; both hang Core / `apply` forever if a server hangs. The CLI `mcp.go:74` already shows the right pattern (`context.WithTimeout(ctx, 30s)`). Apply uniformly to **both** subsystems in one PR.
2. **Goroutine panic recovery** — `agent.go` (parallel batch + hooks + OnEvent), `tools.Runner.Call`, MCP `client.go` readLoop. None have `recover()`. One bug in a tool / hook / event sink kills the process. Add a uniform `safeCall` wrapper.
3. **Permission rule globs** — `permissions.go:138-143` only supports literal `*`. MCP and custom-agent flows both need `mcp:server:*` / `tool.*` globs. Solve once in `ruleToolMatches`, every consumer benefits.
4. **Subprocess process-tree cleanup** — MCP `client.go:161` and (by inheritance) LSP `client.go` use `Process.Kill()` which doesn't kill descendants on Windows. `npx` → `node` orphans. Add `SysProcAttr` setup + a `JobObject` helper on Windows / `Setpgid` + `Killpg` on Unix.

---

## 🔴 CRITICAL — fix before next release

### C1. Orphaned `tool` message in history truncation breaks subsequent LLM call

**Files:** `internal/agent/agent.go:1644-1650`
**Effort:** S
**Closed by:** `16a1d8b`

`truncateMessages` evicts the paired `assistant` message but can keep its `tool` reply alone when the pair-size budget is exceeded but the single-tool size fits. OpenAI/Anthropic both reject `role:"tool"` with no preceding `assistant` carrying matching `tool_call_id` → next LLM call hard-fails the **whole run**, not just the step.

**Fix:** when evicting an `assistant` with `tool_calls`, also evict every following `tool` message until the next non-tool. Add a unit test: build a 10-message history with one assistant+3-tool batch, force the assistant past budget, assert the tools are also gone.

### C2. Parallel tool batch bypasses every circuit breaker

**Files:** `internal/agent/agent.go:1935-2027` (`runParallelToolBatch`)
**Effort:** M
**Closed by:** `16a1d8b`

`runParallelToolBatch` never calls `RecordToolError`, `ResetToolErrors`, `RecordSuccessfulCall`, `IsDuplicateCall`, or `RecordDenied`. Model can spam 16 identical parallel `read` calls forever; 16 erroring tools per step still don't trip `MaxToolErrorRepeats`. Only `MaxSteps` saves the run. Hook denials inside the batch (line 1959) don't increment `deniedPerTool` either.

**Fix:** mirror the serial path's bookkeeping after each parallel child returns. Be careful with concurrency — the circuit-breaker counters need a mutex or the calls need to be folded back on the main goroutine post-batch. Add a regression test that issues a parallel batch of N identical denied calls and asserts the run stops at `MaxDeniedToolRepeats`.

### C3. No panic recovery anywhere in the agent / tools / hooks / event sinks

**Files:** `internal/agent/agent.go:809,855,1958,1992,760,830-836,1995-2002,2006-2013` (and any tool / hook / event sink callsite)
**Effort:** M
**Closed by:** `16a1d8b`

`grep -r 'recover()' internal/` returns zero hits. A `panic` in a tool implementation, a `HooksRunner.RunPreTool/Post`, or any `OnEvent` callback (TUI / CLI / IDE) propagates out of the goroutine that hosts it — for `runParallelToolBatch` that is a child goroutine, so the panic kills the process. Trivial DoS from a buggy tool or UI sink.

**Fix:** introduce a `safeCall` helper in `internal/agent` that wraps a func with `defer recover() + log + convert to error`. Apply at every public boundary where Go code from outside the agent runs (tool dispatch, hook calls, event sinks). Cover with one regression test using a panicking mock tool.

### C4. LSP `initialize` has no timeout — gopls hang blocks Core startup forever

**Files:** `internal/lsp/manager.go:107` (calls `Start(context.Background(), …)`), `internal/lsp/client.go:100` (the inner `request("initialize", ...)`)
**Effort:** S
**Closed by:** `ef174ce`

`Manager.NewManager` calls `Start` with `context.Background()`. A gopls hanging on init (large monorepo without `go.mod`, mis-configured GOPATH, etc.) blocks Runner construction, which blocks `core` / `apply` / `tui` startup. `Close`'s 3s timeout doesn't help because `Close` is never reached.

**Fix:** wrap with `context.WithTimeout(ctx, 30*time.Second)` inside `NewManager` (or `Start`). On timeout, mark the server dead and continue without it. See cross-cutting theme #1.

### C5. MCP `initialize` / `listTools` have the same no-timeout hang

**Files:** `internal/mcp/manager.go:23-33`, `internal/mcp/client.go:174,182,195`
**Effort:** S
**Closed by:** `ef174ce` (also parallelised server start via WaitGroup)

Same shape as C4 but for MCP. Compounded by serial start (BUG-2): three slow MCP servers stack to 90+ seconds. `internal/cli/mcp.go:74` already demonstrates the fix (`30*time.Second` wrap) — generalise.

**Fix:** wrap each `Start` in a per-server `WithTimeout(30s)` and start servers in parallel via `errgroup`. Treat per-server timeout as a non-fatal warning (log + skip), matching today's "non-fatal startup" doc in `Core.New`.

### C6. MCP `Client.send` holds `c.mu` during a blocking `stdin.Write` → deadlock under backpressure

**Files:** `internal/mcp/client.go:231-241` (`send`), `:199` (`Call` re-locks), `:254` (`readLoop` re-locks)
**Effort:** M
**Closed by:** `ef174ce`

If the MCP server is slow to read its stdin (pipe buffer ~64 KiB), `stdin.Write` blocks while holding `c.mu`. Concurrent `Call`s queue on the same mutex. `readLoop`'s response dispatch also needs `c.mu` to look up the pending channel — so the response that would unblock the writer cannot land. Classic deadlock.

**Fix:** split into a separate `writeMu` for the actual `stdin.Write`, OR push writes through a buffered channel served by a dedicated writer goroutine. Add a stress test: 100 concurrent calls against a mock server that pauses 50ms per request — current code deadlocks within seconds.

### C7. MCP / custom-agent permission and tool-allowlist bypass

**Files:** `internal/agent/permissions.go:70-143` (no MCP awareness), `internal/core/core.go:680-687` and `internal/cli/apply.go:577-578` (unconditional MCP injection into `CustomTools`)
**Effort:** M
**Closed by:** `38a47f8`

Two related holes:
1. `subjectForTool` returns "" for any `mcp:*` name (line 73 short-circuits), and `ruleToolMatches` only supports literal `*` — so deny rules like `{tool: "mcp:server:*"}` never match. The only way to deny one MCP tool is to spell its full name exactly.
2. `core.go:684` appends `c.mcpToolDefs()` to a custom agent's tool list unconditionally. A user-declared "reviewer" agent restricted to `[read, grep]` silently gets every MCP tool too.

**Fix:**
1. Teach `subjectForTool` to extract `mcp:<server>:<tool>` shape; teach `ruleToolMatches` to support glob (`path/filepath.Match`).
2. Stop injecting MCP tools into `CustomTools`; pass via `ExtraTools` only when the custom agent does *not* set an explicit `Tools` list, OR only when its `Tools` includes `"mcp:*"` opt-in.

Adds a tests file covering both glob match and the custom-agent allowlist regression.

---

## 🟠 HIGH — sprint work

### H1. Resolver strips structured error context before feeding it to the LLM

**Files:** `internal/agent/agent.go:1496-1503` (`formatApplyErrorCompact`)
**Effort:** S
**Closed by:** `ee7d21e`

Resolver errors (`StaleContent`, `AmbiguousMatch`) carry rich payload: `path`, `range`, `expected_hash`, `actual_hash`, `matches`, `search` preview. The hint that reaches the model is literally `"Файл изменился. Перечитай файл (fs.read) и обнови патч с новым file_hash."` — no path. On a multi-file patch the model cannot tell *which* file changed and re-reads everything (or guesses wrong). One of the largest causes of retry loops in real workflows.

**Fix:** include `path`, `range.start.line`, and (for `AmbiguousMatch`) the matches count + first-match offset in the hint. Keep it ≤ 200 chars to avoid blowing context.

### H2. `extractLSPErrors` injects unbounded diagnostics back into history

**Files:** `internal/agent/agent.go:1509-1535`
**Effort:** S
**Closed by:** `ee7d21e`

A syntax error that cascades into 1000+ parser errors (TS / Go on a stale generated file) produces hundreds of KB of diagnostics injected as a single user message. `MaxPromptBytes` truncation then evicts useful context. Both the user and the model lose.

**Fix:** cap at first 20 errors + `"…N more diagnostics omitted"`. Sort by severity then file:line so the most useful errors survive.

### H3. `cmd.Env` is built without inheriting `os.Environ()`

**Files:** `internal/lsp/client.go:65-67`, similar pattern in `internal/mcp/client.go:266-272`
**Effort:** S
**Closed by:** `751da53` (LSP only — MCP already inherited os.Environ correctly)

`cmd.Env = append(cmd.Env, k+"="+v)` produces an `Env` with ONLY user-supplied entries when `env:` is set — stripping `PATH`, `HOME`, `GOPATH`. Most LSP servers fail to start (no `PATH` for spawned helpers, no `HOME` for cache). When `env:` is empty, `cmd.Env` stays nil and inherits — works. The moment a user sets even one var, everything else vanishes.

**Fix:** `cmd.Env = append(os.Environ(), userVars...)`. Document this in `LSPServerConfig` / `MCPServerConfig`. The fix is identical in both subsystems; do them in one PR.

### H4. LSP column conversion always uses empty `lineText` → wrong column for non-ASCII files

**Files:** `internal/lsp/manager.go:209,235,265,313` (all call `pos.ToLSP(encoding, "")`), `internal/lsp/positions.go:38-53`
**Effort:** M
**Closed by:** `751da53`

UTF-16 column conversion (the LSP default) requires the actual line text. Passing `""` falls through unchanged → Orchestra sends UTF-8 byte offsets to a server expecting UTF-16. ASCII-only files happen to work. Any file with non-ASCII identifiers, strings, or comments before the target column gets the wrong column → definition/references/hover/rename hit the wrong span.

**Fix:** read the line text once per call (the resolver already has the file content), pass it through. Adds a regression test using a file with a Cyrillic identifier.

### H5. `SyncAndDiagnose` returns stale diagnostics from the previous document version

**Files:** `internal/lsp/manager.go:354-379`, `internal/lsp/diagnostics.go:83-92`
**Effort:** M
**Closed by:** `751da53`

After `DidChange` (version N+1), `WaitForUpdate` returns the next `publishDiagnostics` push — which can be a stale push the server queued from version N before processing the change. First post-edit diagnostic call returns the wrong errors.

**Fix:** parse the `version` field from `publishDiagnostics` (newer LSP servers include it); drop pushes with `version < currentDocVersion`. For older servers without `version`, leave a TODO and document the limitation.

### H6. LSP server crash = permanent dead, no restart

**Files:** `internal/lsp/manager.go:107,175` (`serverForPath` returns "no server configured" once `IsDead()`)
**Effort:** M
**Closed by:** `751da53`

A gopls / tsserver crash mid-session loses LSP for the rest of the run. No retry. Combined with C4 (no timeout), a flaky language server perma-degrades the session.

**Fix:** in `serverForPath`, when `IsDead()`, attempt one lazy restart with the original config. Cap to 3 restarts per server per Core lifetime; on the 4th give up and log.

### H7. `DidClose` is never sent after `fs.delete` / `fs.rename`

**Files:** `internal/tools/fs_extra.go:27-63` (FSDelete), `:80-133` (FSRename) — neither talks to LSP
**Effort:** S
**Closed by:** `751da53`

The LSP server keeps a stale open document for a deleted file. Subsequent `definition` / `hover` returns cached old content. Diagnostics for the deleted file persist in `DiagnosticsCache.entries` indefinitely (also a memory leak).

**Fix:** call `Manager.DidClose(relPath)` on delete and `DidClose(old) + DidOpen(new)` on rename. Drop the URI from `DiagnosticsCache` at the same time.

### H8. `write_atomic` accepts unlimited `Content` size → OOM vector

**Files:** `internal/resolver/external_patches.go:239-248`, `internal/applier/ops_applier.go:236`
**Effort:** S
**Closed by:** `a073aa6`

A 2 GB malicious / buggy patch `{type:"file.write_atomic", content:"…"}` is read straight into `[]byte` and written. No cap anywhere. Realistic attack vector: prompt-injected `fs.write` with a giant blob.

**Fix:** hard limit at schema level (say 32 MiB) with a clear error. Hardly affects legitimate patches; closes the OOM door.

### H9. Concurrent `apply` runs clobber each other's `.orchestra.bak`

**Files:** `internal/applier/ops_applier.go:301-310`, `:497-552`
**Effort:** M
**Closed by:** `a073aa6` (in-process mutex + safer cross-FS fallback; per-project file lock deferred)

Two concurrent `apply` invocations on the same file each rename their temp into `path+".orchestra.bak"` — the second clobbers the first. The original-before-first-edit is permanently lost. Cross-FS rename fallback (`ops_applier.go:545-547`) is also non-atomic (`Remove → Rename`); a second failure means the target is gone with no recovery.

**Fix:** per-project advisory file lock around the full apply (POSIX `flock`, Windows `LockFileEx`). Cross-FS rename fallback: copy-then-rename within the same dir, never `Remove+Rename`.

### H10. Unified diff applier silently corrupts line endings + ignores hunk counts

**Files:** `internal/resolver/external_patches.go:577-578` (CRLF→LF collapse), `:673-707` (`oldCount`/`newCount` parsed but ignored, `_ = oldCount; _ = newCount` is explicit dead code at `:706-707`), `:638-639` (`\ No newline at end of file` skipped, not honoured)
**Effort:** M
**Closed by:** `a073aa6`

Three related bugs in `applyUnifiedDiff`:
- File-on-disk CRLF gets silently converted to LF on every apply → spurious diffs in the next commit.
- Malformed hunks (`-3 +1` header but 5 actual `-` lines) apply anyway — corrupts the file.
- "no trailing newline" hint ignored → silently adds a newline where none should be.

**Fix:** detect CRLF on read, preserve through write. Validate hunk counts; reject mismatches with `InvalidParams`. Honour `\ No newline at end of file`.

### H11. `task_result` short-circuits the main agent

**Files:** `internal/agent/agent.go:571-582`
**Effort:** S
**Closed by:** `ee7d21e` + `a48d044` (spawn-site IsChild wiring)

The `task_result` tool returns `Result{SubtaskResult: req.Content}` unconditionally — no guard that the agent is a child (subtask / skill). A confused main-mode model can terminate the run by emitting `task_result`, and the caller sees only that string as the answer. The system prompt advises against it but nothing enforces it.

**Fix:** add `agent.Options.IsChild bool`. `task_result` returns an "ignored: this tool is for child agents only" tool result when not a child, instead of terminating. Wire `IsChild=true` from `subtask` / `skill_invoke` spawn sites.

### H12. Server-name with `:` silently misroutes MCP calls

**Files:** `internal/mcp/manager.go:111-118` (`parseMCPToolName`), `internal/config/config.go:170-179` (no validation)
**Effort:** S
**Closed by:** `e4b3d92`

`parseMCPToolName` uses `SplitN(name, ":", 3)` and trusts the middle field as the server name. A user-supplied `name: "ns:foo"` produces tool `mcp:ns:foo:bar` → parsed as server `ns`, tool `foo:bar`, `findClient("ns")` returns nil, every call fails with `"mcp server "ns" not found"` — but `Tools()` still advertises the bogus name to the model, which keeps trying.

**Fix:** validate `MCPServerConfig.Name` at `config.Load`: reject `:`, enforce uniqueness within `cfg.MCP.Servers`. Fail-fast at config load.

### H13. Windows MCP / LSP process-tree zombies on `Process.Kill`

**Files:** `internal/mcp/client.go:161` (and any future LSP shutdown that uses `Process.Kill`)
**Effort:** M
**Closed by:** `e4b3d92` (MCP only — LSP shutdown still uses Process.Kill; tracked as follow-up)

`npx -y @modelcontextprotocol/server-foo` spawns `node` underneath. Killing `npx.cmd` leaves an orphan `node.exe` on Windows. No `JobObject` wiring. Linux has the same shape if a server spawns its own children — `exec.Cmd` does not set `Setpgid` here.

**Fix:** Windows path — use `JobObject` (Go has `golang.org/x/sys/windows.NewJobObject`) and assign on Start. Unix path — `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}` and `syscall.Kill(-pgid, SIGTERM)` on shutdown.

### H14. `Position.Col` byte/rune contract is undocumented in the prompt

**Files:** `internal/ops/ops.go:22` (says "0-based UTF-8 byte offset"), `internal/resolver/external_patches.go:549` (uses byte offsets), prompt files `internal/prompt/files/*.txt`
**Effort:** S
**Closed by:** verified N/A — LLM never emits `Position.Col` directly; resolver derives all positions from text matching (`internal/resolver/external_patches.go:549` only consumes its own offset output). No prompt change needed for current architecture.

Resolver uses byte offsets correctly. But if the LLM-facing prompt does not explicitly state "byte offset, not rune offset," a 4-byte emoji at column 0 in the file becomes column 4 in the op the LLM produces. Resolver applies the op → file corrupts.

**Fix:** audit `internal/prompt/files/build.txt` (and other prompt files) to confirm they specify "byte offset". If not, add explicit wording.

### H15. Compaction can fail to converge → wasted LLM calls until `MaxSteps`

**Files:** `internal/agent/agent.go:360-371` (`compactHistory` call site), `internal/agent/compact.go`
**Effort:** M
**Closed by:** `ee7d21e` (min-shrink ≥20% guard + fall through to truncateMessages on non-convergence)

After `compactHistory` returns, the synthetic summary message is not marked as already-compacted. If the summary itself still exceeds `MaxPromptBytes * pct/100`, the next loop iteration compacts again — summary-of-summary every step, each burning an LLM call for no progress until `MaxSteps` trips. No minimum-shrink check either (line 367 logs the regression but uses the result anyway).

**Fix:** sentinel marker on compacted message (or compare new vs old size before accepting); when compaction does not shrink enough, fall back to evicting oldest pairs instead of re-summarising.

---

## 🟡 MEDIUM — backlog

Grouped by subsystem to make sprint-planning easy.

**Sprint 3 closure summary (all 33 MEDIUM items triaged):**

| Cluster | Closed | Deferred (with rationale) | Commit |
|---|---|---|---|
| Agent (M1–M8) | M1, M2, M3, M4, M5, M6, M7, M8 | — | `d46e10f` |
| Resolver/Applier (M9–M16) | M9, M10, M11, M12, M14, M15, M16 | M13 (partial via H1; full needs `findUnique` refactor) | `27a335a` |
| LSP (M17–M24) | M17, M18, M19, M20, M21, M22, M23 | M24 (already closed by H7) | `ebaa992` |
| MCP (M25–M33) | M25 (via H12), M28, M29, M30, M32, M33 | M26 (auto-restart needs design), M27 (per-call timeout — config schema work), M31 (per-tool allowlist — config schema work) | `10d6f96` |

The detailed per-item entries below are the original audit findings and
remain for traceability — they aren't individually annotated to keep the
doc readable. Reference the commit hashes above and the per-commit
messages for the exact code changes.

### Agent layer

| ID | File:line | Issue | Effort |
|---|---|---|---|
| M1 | `agent.go:171,1995-2013` | `OnEvent` doc says "must not block" but parallel batch fires from 16 goroutines; doc must add "MUST be goroutine-safe". | S |
| M2 | `agent.go:1670-1689` | `estimateMessageSize` undercounts (images = 4 KB regardless of real size, tool-call JSON +50 placeholder). Real prompt overshoots `MaxPromptBytes`. | S |
| M3 | `agent.go:354` | No `ctx.Err()` check at loop head; cancellation only honoured when next LLM call sees it. Minor responsiveness gap. | S |
| M4 | `agent.go:1286-1289` | When provider returns no `Usage`, tracker silently records 0 — `usage.jsonl` understates tokens/cost. | S |
| M5 | `agent.go:497,594` + `internal/usage` | `UsageRecorder` impl must be goroutine-safe (subtask + skill spawn the tracker concurrently). Add doc + sanity audit. | S |
| M6 | `agent.go:900` | `ResetFinalFailures` on every success → "fail, read, fail, read" never trips `MaxFinalFailures`. Cap is cosmetic. | S |
| M7 | `agent.go:408-423,1107-1113` | Invalid-step paths (`step.Tool == nil`, default branch) `continue` without counter increment. Only `MaxSteps` bounds them. | S |
| M8 | `internal/agent/circuit_breaker.go:131` | `RecordInvalid` is dead code (never called). Either wire from `nextStep`'s error branch or delete. | S |

### Resolver / applier

| ID | File:line | Issue | Effort |
|---|---|---|---|
| M9 | `external_patches.go:539-553` (`posFromOffset`) | Subtle edge-case behaviour with EOF newlines; works but undocumented. Add comments + unit tests for `"a\n"`, `""`, `"a"` corner inputs. | S |
| M10 | `ops_applier.go:528` | `tmp.Sync()` syncs the file but not the parent dir. On POSIX, rename durability requires `fsync(parent_dir)` after rename. Linux crash window. | S |
| M11 | `applier/types.go:13-14` (`FileDiff.Before/After`) | Diff preview holds full file content as strings. A 100 MB file with 1-line change → 200 MB string data, blows RPC transport limits. Cap at e.g. 64 KB per side. | M |
| M12 | applier | No binary-file detection. Edits to `*.png` round-trip through JSON, fail with cryptic encoding errors. Add NUL-byte sniff in first 8 KB and reject. | S |
| M13 | `agent.go:1500-1501` | `AmbiguousMatch` hint says "уточни search-блок" without listing the conflicting line numbers the resolver already knows. Add them. | S |
| M14 | `external_patches.go:691-694` | `parseUnifiedDiff` doesn't track file context for multi-file diffs → may merge hunks across files. | M |
| M15 | `external_patches.go:646-655` | `applyUnifiedDiff` missing trailing-newline handling when adding lines to an empty file. | S |
| M16 | `ops_applier.go:145` (`mkdirByPath`) | Dedupe by canonical-rel does last-write-wins for `Mode`; two ops with different `Mode` for the same path silently use one. Reject conflict instead. | S |

### LSP

| ID | File:line | Issue | Effort |
|---|---|---|---|
| M17 | `manager.go:364-378` | `SyncAndDiagnose` swallows `DidChange` errors → caller assumes "all clean". Propagate. | S |
| M18 | `client.go:77` | Server stderr → `io.Discard`. Gopls crash dumps, "missing go.mod", license errors silently dropped. Pipe to a ring buffer surfaced via `core.health` or a debug log. | M |
| M19 | `client.go:344-357` | `notifyCh` capacity 256 with drop-oldest. Large workspace init → notifications silently dropped. Add at least a counter so the operator can see drops happened. | S |
| M20 | `manager.go:383-403` (`locsToTool`) | Doesn't filter out-of-workspace results. Stdlib definitions come back with `../../usr/local/go/...` relative paths that downstream tools reject as path-traversal. | S |
| M21 | `client.go:299-359` | `Close` does not wait for `readLoop` exit; in-flight callers may block on their own ctx until timeout. Add a `readLoopDone` wait in `Close`. | S |
| M22 | `runner.go:165-169` | `lsp.NewManager` errors dropped on the floor (`mgr, _ := …`). Operator has no way to know gopls failed to launch. Log them. | S |
| M23 | `client.go:194` | `notify` ignores ctx entirely. If writer is stuck, ctx cancellation is no-op. Add ctx-aware write. | S |
| M24 | `client.go:265-271` | `DidClose` doesn't clear the `DiagnosticsCache.entries[uri]` — long sessions leak. | S |

### MCP

| ID | File:line | Issue | Effort |
|---|---|---|---|
| M25 | `manager.go:60-77` + `config.go:170` | Duplicate server names silently produce duplicate `mcp:dup:foo` tools. `findClient` returns only the first; the second's tools silently misroute. (Caught by H12 once name validation lands.) | S |
| M26 | `manager.go:107` (lifecycle) | No health check / crash detection / auto-restart of a dead MCP server. Tools cache still advertised. | M |
| M27 | `runner.Call` → MCP | No per-call timeout. A 10-min MCP tool ties up the whole agent step. Add `MCPServerConfig.CallTimeoutS`. | S |
| M28 | `client.go:142-147` | Non-text content (`image`, `resource`, `audio`) silently dropped; the wrapper `{"result": "..."}` is a non-standard shape. Document the limitation or pass through structured. | M |
| M29 | `client.go:251-253` | Server-initiated notifications (`notifications/tools/list_changed`) silently dropped. Cached `c.tools` becomes stale; agent gets `unknown tool` errors. Handle the notification, refresh the cache. | M |
| M30 | `client.go:96` | 4 MiB scanner buffer; oversize message → `bufio.ErrTooLong`, readLoop silently exits, all subsequent calls fail with `"exited"`. No log of the reason. | S |
| M31 | `config.go:169-179` | Only `Disabled bool` per server. No per-tool allowlist on a server. (Mitigated by C7's glob in permission rules but worth a first-class field.) | M |
| M32 | `client.go:266-272` | `os.Environ()` inherited unconditionally + `os.ExpandEnv` runs on user values. `env: { KEY: "$AWS_SECRET_ACCESS_KEY" }` exfiltrates the secret to whatever the MCP server logs. Per `CLAUDE.md` safety rules, secrets must not leak — add explicit allowlist or pass-through opt-in. | M |
| M33 | `client.go:155-166` | Kill-after-timeout error and `cmd.Wait()` error are returned but `manager.go:48` discards them with `_ = c.Close()`. Zombie / failed-kill is invisible. | S |

---

## 🟢 LOW — when convenient

| ID | File:line | Issue |
|---|---|---|
| L1 | `agent.go:783` | `AgentLogger.LogToolCall` deref without nil guard in serial path (parallel path guards). Confirm `*llm.Logger.LogToolCall` is nil-safe. |
| L2 | `agent.go:706` | `plan_exit` auto-approves when `QuestionAsker == nil`. CI runs with `--plan-only` could silently flip mode. |
| L3 | `agent.go:1498,1501` | Russian-language hints; model may be tuned to English. Internationalise or move to `internal/prompt/files`. |
| L4 | `external_patches.go:83` | `search_replace` with `Search == Replace` is a no-op accepted silently. Reject as invalid. |
| L5 | `ops_applier.go:540,550` | `os.Chmod` is best-effort and a no-op on Windows. Document. |
| L6 | `lsp/positions.go:11-17` | `PathToURI` doesn't URL-escape spaces / `#` / `?`. Gopls tolerates spaces; some servers don't. |
| L7 | `lsp/client.go:101` | `"processId": 0` in `initialize`. LSP spec says integer or null. Use `os.Getpid()` so servers can monitor parent death. |
| L8 | `lsp/client.go:339-342` | Server→client requests answered with `null`. `workspace/configuration` expects an array — special-case it. |
| L9 | `mcp/client.go:74-81` | MCP server stderr inherited by Orchestra process. Mixes with TUI / JSON-RPC over stdout. Capture or redirect. |
| L10 | `lsp/client.go:299-359` (`readLoop` panic) | No `recover()` — covered by C3 if implemented uniformly. |

---

## Suggested execution order

1. **Sprint 1 (~2 days):** all CRITICAL (C1–C7) — they cluster around three themes (timeouts, panic safety, permissions) that share code paths. **DONE — see commits `16a1d8b`, `ef174ce`, `38a47f8`, `c11d98c`.**
2. **Sprint 2 (~3 days):** all HIGH (H1–H15) — five thematic clusters (LLM efficiency, applier data-safety, agent contracts, LSP correctness, MCP cross-cutting). **DONE — see commits `ee7d21e`, `a073aa6`, `a48d044`, `751da53`, `e4b3d92`.** H14 verified N/A.
3. **Sprint 3 (~1 day):** all MEDIUM (M1–M33) except four deferred. **DONE — see commits `d46e10f`, `27a335a`, `ebaa992`, `10d6f96`.** Deferred: M13 (partial), M26, M27, M31.
4. **LOW** when touching the file for unrelated reasons.

## Out of scope for this plan

- ProtocolVersion bump (stays at 4 for now — every fix above is backwards-compatible in wire shape).
- `$/cancelRequest` cancellation — already implemented in commit `9805f0e`.
- 13 items closed in the previous sanity-fix series (commit `065d6e0..1d172ad`).

## How to use this doc

When starting a fix, copy the relevant item's heading into a task / PR title. Reference its ID (`C1`, `H7`, `M14`) in commits so the audit lineage stays traceable. After landing, add a one-line "Closed by <commit>" at the end of the item below — do not delete entries; keep the doc as the audit ledger.
