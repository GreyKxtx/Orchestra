# Cross-platform consistency cleanup

## Current state (audit)

| Layer | Status |
|---|---|
| Path normalization (slash/backslash, case-insensitivity, `\\?\`) | ✅ Done — runtime.GOOS branches in `tools/runner.go`, `lsp/positions.go` |
| Atomic write rename semantics | ✅ Done — `applier/ops_applier.go` |
| System prompt OS snapshot | ✅ Done — `prompt/snapshot.go` injects GOOS/GOARCH into prompt |
| `exec.run` shell wrapping | ⚠️ Direct `exec.Command(args[0], args[1:]...)` — model must know OS shell |
| Hooks config | ❌ User writes raw command; no OS-aware defaults or validation |
| Default `.orchestra.yml` template (`init`) | ❌ Same template emitted on all OSes |

## What to add

### 1. exec.run optional shell wrapping

Add an optional `shell: true` field in the input schema. When true:

- On Windows: wrap in `cmd /c <command> <args...>`
- On Unix: wrap in `/bin/sh -c "<command> <args...>"`

Lets the model write portable code like `{"shell": true, "command": "echo foo > bar.txt"}` instead of figuring out platform-specific quoting.

### 2. Hooks: OS-aware defaults in `orchestra init`

`orchestra init` emits a default `.orchestra.yml`. Today the hooks section is the same on every OS. Detect `runtime.GOOS` and emit the appropriate variant:

```yaml
# Windows
hooks:
  enabled: false
  pre_tool: [cmd, /c, echo [HOOK-PRE] %ORCH_TOOL_NAME% ^>^> .orchestra\hooks.log]

# Linux / macOS
hooks:
  enabled: false
  pre_tool: [sh, -c, "echo [HOOK-PRE] $ORCH_TOOL_NAME >> .orchestra/hooks.log"]
```

### 3. Hook validator at startup

If `hooks.enabled: true`, run a fast smoke-test of `pre_tool` once at agent boot (with `ORCH_TOOL_NAME=__bootcheck__`). If it returns non-zero or times out, emit a clear warning in the TUI: "hook misconfigured — Windows uses cmd, Linux uses sh". Don't fail — just warn.

### 4. Bundled hook helpers

Ship `scripts/orch-hook.sh` and `scripts/orch-hook.cmd` so users can write:

```yaml
hooks:
  pre_tool: [orch-hook, audit-only]
```

The shipped wrapper handles OS-specific logging, JSON validation, and standard exit codes. Users don't write shell commands at all.

## Out of scope

- Bash vs zsh vs fish — we always invoke `/bin/sh` (POSIX) which all of them are compatible with for our hook use case
- WSL detection — runs as Linux from our perspective
- Cygwin/MSYS2 paths — extremely rare in target audience, defer until a user reports

## Priority

Medium. Current state works for Windows (paths handled), and the `init` template is the only place a casual user would notice. Cleanup is mostly polish, not a correctness fix.
