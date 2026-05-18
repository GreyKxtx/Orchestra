<safety-invariants>
These are project-wide hard rules. Treat them as binding; do not relax any of them without explicit user instruction in the current task.

**Filesystem**
- Never read or write outside `project_root`. Resolve symlinks/junctions and fail closed on escape.
- All on-disk writes are **atomic**: write to a temp file → fsync/close → rename. No partial files.
- Apply is **dry-run by default**. Mutate disk only when `--apply` is set or `agent.run apply: true`. On write, backup to `*.orchestra.bak`.

**Patch model (two layers — do not mix)**
- LLM-facing **External Patches**: `file.search_replace`, `file.unified_diff`, `file.write_atomic`. Every patch carries `file_hash` (sha256) of the version the LLM read.
- Disk-facing **Internal Ops**: `file.replace_range`, `file.write_atomic`, `file.mkdir_all`. 0-based, end-exclusive coordinates. Every mutating op carries `conditions.file_hash`; the applier re-checks before writing.
- The agent never emits internal ops directly — `internal/resolver` is the bridge.

**Anchors / stale content**
- `file.replace_range` requires both `before_anchor` and `after_anchor`. Ambiguous or missing match → fail with diagnostic, not best-effort.
- Stale `file_hash` → return `ErrStaleContent`; the agent loop hints and retries with a fresh read.

**Execution**
- `exec.run` requires explicit consent: `--allow-exec` on the CLI, or `exec.confirm: false` in config.
- Non-interactive (stdin closed), time-boxed via `context.WithTimeout`, output capped. Never spawn processes without a cancellation path.

**Go engineering**
- No panics for expected failures. Errors wrap with `fmt.Errorf("...: %w", err)`.
- Concurrency uses `context.Context` + `sync.Mutex/RWMutex`. Every goroutine must have a stop path.
- No global mutable state.

**Protocol versioning**
- JSON-RPC method names and params are part of the contract. Bump `ProtocolVersion` / `OpsVersion` / `ToolsVersion` (in `internal/protocol/version.go`) rather than silently changing them — and update `docs/PROTOCOL.md` in the same commit.
- Top-level JSON-RPC arrays (batch) are not supported — return `-32600` with `id: null`. `id: null` is a request, not a notification.
</safety-invariants>
