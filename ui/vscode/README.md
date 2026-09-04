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

General · **Providers** (provider catalog + API models, vision toggle) · Index & Graph · Agent · Tools & MCP

### Commands

- **Orchestra: Open Chat**
- **Orchestra: Settings**
- **Orchestra: Ping Core**

**Protocol v13** — see `docs/PROTOCOL.md`. Handshake: `protocol_version=13`, `ops_version=1`, `tools_version=14`. `orchestra version` prints all three.

## Run (Cursor / VS Code)

1. Install **Orchestra AI Code** from Marketplace (or VSIX)
2. Open a **folder** (File → Open Folder) — core needs a project root
3. Launch chat — any of:
   - **Activity Bar** (left): icon **Orchestra** (chat bubble) → sidebar chat
   - **Status bar** (bottom-right): click **Orchestra**
   - **Command Palette** (`Ctrl+Shift+P`): `Orchestra: Open Chat`
   - **Shortcut**: `Ctrl+Shift+O` (`Cmd+Shift+O` on macOS)
   - **Editor title bar**: chat icon when a file is open
4. First run: configure LLM in **Orchestra: Settings** (LM Studio / OpenAI-compatible API)
5. Debug wire: **Output → Orchestra**

There is **no separate Cursor-style floating panel** — chat lives in the **left sidebar** (Activity Bar → Orchestra). The editor-panel fallback remains if the sidebar view is unavailable.

Restart / **Developer: Reload Window** after updating the extension.

### Dev (F5 from repo)

1. `go build -o orchestra.exe ./cmd/orchestra` in repo root
2. `cd ui/vscode` → `npm install` → `npm run compile`
3. Open Orchestra workspace → **Run Orchestra Extension** (F5)

Restart Extension Host after rebuilding `orchestra.exe` if the binary was locked.

## Package / Marketplace

Bundled core layout (inside `.vsix`):

```
bin/win32-x64/orchestra.exe
bin/linux-x64/orchestra
bin/darwin-arm64/orchestra
bin/darwin-x64/orchestra
```

Extension auto-picks `bin/<platform>-<arch>/` for the current machine. Override with **Settings → Orchestra: Binary Path**.

### Build VSIX locally

```bash
cd ui/vscode
npm ci
npm run bundle:core        # current OS only (F5 dev)
npm run package            # → orchestra-*.vsix
```

Cross-platform **fat VSIX** (all four cores) — use CI, not `bundle:core:all` from Windows (cgo/tree-sitter):

```bash
# Tag push or manual run in GitHub Actions → vscode-vsix workflow
git tag vscode-v0.1.0 && git push origin vscode-v0.1.0
```

Or **Actions → vscode-vsix → Run workflow** (optional **Publish** if `VSCE_PAT` / `OVSX_PAT` are set).

Matrix builds natively on `windows-latest`, `ubuntu-latest`, `macos-latest` (arm64 + x64 cross), merges into `bin/`, packages `.vsix`, uploads artifact **orchestra-vsix**.

### Publish checklist

1. **Publisher** — [Marketplace publisher](https://code.visualstudio.com/api/working-with-extensions/publishing-extension#create-a-publisher) + [Open VSX namespace](https://open-vsx.org/) (`Screamgxne`); `publisher` in `package.json` must match.
2. **Version** — bump `version` in `package.json` (semver).
3. **Assets** — icon `images/icon.png` (from repo `logo.png`), README, LICENSE, repo URL.
4. **Fat VSIX** — run **vscode-vsix** workflow (native matrix build); do not rely on `bundle:core:all` from a single dev machine.
5. **GitHub secrets**
   - `VSCE_PAT` — Microsoft [Marketplace](https://marketplace.visualstudio.com/) (Azure DevOps PAT, Marketplace publish scope)
   - `OVSX_PAT` — [Open VSX](https://open-vsx.org/) → Profile → Access Tokens (for **Cursor** / VSCodium)
6. **Publish** — tag `vscode-v0.1.2`, or workflow_dispatch with **Publish** checked; CI runs `vsce publish` + `ovsx publish`.
7. **Open VSX review** — new extensions may show **Deactivated** until Eclipse/Open VSX approves the namespace/extension; Cursor won't list it until active.
8. **LLM config** — bundled core ≠ LLM: users still configure provider/API in Orchestra Settings (LM Studio, OpenAI-compatible, etc.).

CI: TypeScript compile on every push (`.github/workflows/ci.yml`); full multi-platform VSIX on tag / workflow_dispatch (`.github/workflows/vscode-vsix.yml`).

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
