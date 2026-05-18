# Hooks

Pre- and post-tool hooks run an arbitrary subprocess around every tool call.
Useful for audit logging, custom denylists, or pairing tool calls with project
automation (e.g. auto-format after `edit`).

## Config

```yaml
hooks:
  enabled: true
  pre_tool:  [./scripts/audit.sh]      # gate every tool call; exit non-zero to deny
  post_tool: [./scripts/notify.sh]     # observe every successful tool call
  timeout_ms: 5000                     # per-hook subprocess timeout (default 5000)
```

* `pre_tool` — non-zero exit blocks the tool call. The model receives a
  `TOOL_ERR` with the hook's stderr output and may try a different approach.
* `post_tool` — non-zero exit is logged via `log.Printf` but does NOT fail
  the tool. Use for non-blocking notifications.
* Both fields are command + args arrays (`[program, arg1, arg2, ...]`).
* Disabled by default. Set `enabled: true` AND configure at least one command.

## Environment

Each hook subprocess receives:

| Variable             | Value                                            |
| -------------------- | ------------------------------------------------ |
| `ORCH_TOOL_NAME`     | canonical tool name (e.g. `read`, `bash`, `edit`)|
| `ORCH_TOOL_INPUT`    | the tool's JSON input (pre) / output (post)      |
| `ORCH_WORKSPACE_ROOT`| absolute path to the project root                |

Working directory is the workspace root. stdout/stderr inherit unless your
script redirects.

## Examples

**Audit every tool call to a log file:**

```bash
#!/bin/sh
printf '%s\t%s\t%s\n' "$(date -u +%FT%TZ)" "$ORCH_TOOL_NAME" "$ORCH_TOOL_INPUT" \
  >> "$ORCH_WORKSPACE_ROOT/.orchestra/audit.log"
```

**Block writes outside a whitelist of paths:**

```bash
#!/bin/sh
case "$ORCH_TOOL_NAME" in
  write|edit|fs.delete|fs.rename) ;;
  *) exit 0 ;;
esac
path=$(printf '%s' "$ORCH_TOOL_INPUT" | jq -r .path)
case "$path" in
  src/*|tests/*) exit 0 ;;
  *) echo "writes outside src/ and tests/ are blocked" >&2; exit 1 ;;
esac
```

**Auto-format after edits:**

```bash
#!/bin/sh
case "$ORCH_TOOL_NAME" in
  write|edit) ;;
  *) exit 0 ;;
esac
path=$(printf '%s' "$ORCH_TOOL_INPUT" | jq -r .path)
case "$path" in
  *.go) gofmt -w "$path" 2>/dev/null ;;
  *.py) ruff format "$path" 2>/dev/null ;;
esac
```

## Verification

`go test ./internal/hooks/...` covers the subprocess transport (env vars,
timeouts, denial). `go test ./tests/e2e_agent/ -run Hook` covers the end-to-end
agent wiring (every tool call really does invoke the hook, denial really does
block the tool).
