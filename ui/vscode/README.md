# VS Code / Cursor extension

JSON-RPC stdio client for `orchestra core`. Webview chat + in-panel Settings + Ping smoke.

## Architecture

```
Webview (chat / settings UI)
    ↕ postMessage
Extension Host (CoreSession)
    ↕ Content-Length JSON-RPC
orchestra core (subprocess)
```

| Layer | Role |
|---|---|
| `src/chat/` | Webview panel — chat + settings (projection only) |
| `src/coreSession.ts` | Long-lived core: ensure → initialize → session.* + runtime.* |
| `src/rpc/` | Framing + JSON-RPC (notifications + server requests) |
| `src/protocol/events.ts` | `agent/event` + host↔webview message types |

**Lifecycle:** spawn once → `initialize` → `session.start` → many `session.message`. Business logic stays in core.

## What works

### Chat

- **Orchestra: Open Chat** — Cursor-like composer, sessions, streaming, tool chips
- **Attachments / vision** — drag-drop, paste, `@` mentions; protocol v13 `attachments[]`
- **Dry-run / Apply toggle** — `session.message` with `apply: true|false`
- **Pending ops bar** — per-file review (↑↓ · a/x · Enter), filtered `ops.apply`, or Apply All via `session.apply_pending`
- **LSP install modal** — dedicated UI for `lsp.install` permission requests
- **Permission / Question modals** — `permission/request`, `question/ask`
- Mode pills → `mode`; Effort/Fast → `profile`
- Model picker → `runtime.list_providers` + `runtime.set_model`

### Settings (in-panel, 6 tabs)

General · **Models** (provider catalog + API models, vision toggle) · Index & Graph · Agent · Tools & MCP · Plugins

### Commands

- **Orchestra: Open Chat**
- **Orchestra: Settings**
- **Orchestra: Ping Core**

**Protocol v13** — see `docs/PROTOCOL.md`. Handshake: `protocol_version=13`, `ops_version=1`, `tools_version=12`.

## Run (Cursor / VS Code)

1. `go build -o orchestra.exe ./cmd/orchestra` in repo root
2. `cd ui/vscode` → `npm install` → `npm run compile`
3. Open Orchestra workspace → **Run Orchestra Extension** (F5)
4. **Orchestra: Open Chat** → type a message
5. Debug wire: **Output → Orchestra**

Restart Extension Host after rebuilding `orchestra.exe` if the binary was locked.

## Package / Marketplace

Build a `.vsix` locally:

```bash
cd ui/vscode
npm ci
npm run compile
npm run package   # produces orchestra-*.vsix
```

**Before publishing:**

- Bump `version` in `package.json`
- Ensure `orchestra` is on PATH on target machines, or document install (bundled binary in the VSIX is not shipped yet — users build from source or install the CLI separately)
- Publish: `npx @vscode/vsce publish` (requires Azure DevOps publisher token)

CI builds the extension on every push (`.github/workflows/ci.yml` → `vscode-extension` job).

## Layout

```
ui/vscode/
  media/chat.css|js       # chat webview
  media/settings.css|js   # settings webview
  src/extension.ts
  src/coreSession.ts
  src/chat/panel.ts
  src/chat/settings.ts
  src/protocol/events.ts
  src/rpc/
```

## Still TODO (extension)

- Rich expandable tool blocks + inline diff (full TUI parity)
- Reasoning/thinking block UI
- Plugins install UI; hooks/git/browser settings editors
- Optional: bundle `orchestra` binary inside the VSIX for zero-setup install
