# Browser Tools Design

## Goal

Add full browser automation to the Orchestra agent via the official Playwright MCP server (`@playwright/mcp`). The agent can navigate pages, read their content as structured accessibility trees, click, type, fill forms, take screenshots, and evaluate JavaScript — covering both web app testing and web research use cases.

## Architecture

The Playwright MCP server runs as a long-lived subprocess managed by a new `internal/browser.Client`. Orchestra communicates with it over stdio using the MCP JSON-RPC protocol (identical to the protocol used by Cursor and Claude Code). One browser instance persists for the lifetime of an agent session — page state is preserved between tool calls.

```
Runner.browserClient (*browser.Client)
    ↓ lazy init on first browser tool call
internal/browser/Client
    ↓ MCP JSON-RPC over stdin/stdout
npx @playwright/mcp (Node.js subprocess)
    ↓ Chrome DevTools Protocol
Chromium (headless by default, visible for debugging)
```

## File Map

| File | Change |
|---|---|
| `internal/browser/client.go` | **Create**: MCP subprocess client (start, call, close) |
| `internal/browser/client_test.go` | **Create**: tests with mock MCP subprocess |
| `internal/tools/browser.go` | **Create**: 10 browser tool implementations |
| `internal/tools/browser_test.go` | **Create**: unit + integration tests |
| `internal/config/config.go` | **Modify**: add `BrowserConfig` struct + `Browser BrowserConfig` field to `Config` |
| `internal/tools/runner.go` | **Modify**: add `browserClient *browser.Client` field, init in `NewRunner`, close in `Close()` |
| `internal/tools/registry.go` | **Modify**: add 10 `toolBrowser*()` defs + register when `allowBrowser=true` |
| `internal/tools/call.go` | **Modify**: add dispatch cases for all 10 browser tools |
| `internal/config/config.go` | **Modify**: add browser tool names to `validAgentToolNames` |
| `internal/protocol/version.go` | **Modify**: bump `ToolsVersion` 7 → 8 |

## Configuration

Added to `.orchestra.yml`:

```yaml
browser:
  headless: true          # false shows a visible browser window (debugging)
  timeout_ms: 30000       # per-operation timeout in milliseconds
  viewport_width: 1280    # viewport dimensions
  viewport_height: 720
  allow_eval: false       # explicitly opt-in to browser.eval (JS execution)
```

`BrowserConfig` struct in `internal/config/config.go`:

```go
type BrowserConfig struct {
    Headless       bool `yaml:"headless"`
    TimeoutMS      int  `yaml:"timeout_ms"`
    ViewportWidth  int  `yaml:"viewport_width"`
    ViewportHeight int  `yaml:"viewport_height"`
    AllowEval      bool `yaml:"allow_eval"`
}
```

Defaults: `headless: true`, `timeout_ms: 30000`, `viewport_width: 1280`, `viewport_height: 720`, `allow_eval: false`.

## Permission Gating

Browser tools require a new `allowBrowser bool` parameter in `ListTools()`, mirroring `allowExec` and `allowWeb`. All 10 tools are absent when `allowBrowser=false`.

In the CLI: `--allow-browser` flag (analogous to `--allow-exec` and `--allow-web`).

`browser.eval` has an additional guard: `cfg.Browser.AllowEval` must be `true` at call time, even when `allowBrowser=true`. This separates "can use the browser" from "can run arbitrary JS."

## MCP Protocol

### Subprocess command

```
npx --yes @playwright/mcp@latest --headless --viewport-size "1280,720"
```

Flags passed based on `BrowserConfig`. If `npx` is not found on PATH, all browser tool calls return a clear error: `"browser tools require Node.js and npx; install from https://nodejs.org"`.

### Handshake (once on first call)

```json
→ {"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"orchestra","version":"1"}}}
← {"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"playwright"}}}
→ {"jsonrpc":"2.0","method":"notifications/initialized"}
```

### Tool call

```json
→ {"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"browser_navigate","arguments":{"url":"https://example.com"}}}
← {"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"Navigated to https://example.com"}]}}
```

### Error handling

- `npx` not found → `protocol.ExecFailed` with install hint
- Subprocess exits unexpectedly → attempt one restart; if it fails again, return error
- Operation timeout → `protocol.ExecTimeout`
- MCP error response → `protocol.ExecFailed` with stderr

## Tool Set

### 1. `browser.navigate`
Navigate to a URL and wait for the page to load.

**Input:**
```json
{"url": "https://example.com", "wait_until": "load"}
```
`wait_until`: `"load"` (default) | `"domcontentloaded"` | `"networkidle"`

**Output:** `{"url": "https://example.com", "title": "Example Domain"}`

---

### 2. `browser.snapshot`
Return the page's accessibility tree as structured text. This is the primary way for the LLM to "read" a page — no HTML, just semantic structure with `ref` IDs for subsequent clicks/interactions.

**Input:** `{}`

**Output:**
```json
{
  "snapshot": "- heading \"Example Domain\" [level=1]\n- paragraph \"This domain is...\"\n- link \"More information\" [ref=e12]"
}
```

---

### 3. `browser.screenshot`
Capture the current page as a PNG screenshot (base64-encoded).

**Input:** `{"full_page": false}`

**Output:** `{"image": "<base64 PNG>", "width": 1280, "height": 720}`

---

### 4. `browser.click`
Click an element identified by its accessible name or `ref` from a snapshot.

**Input:**
```json
{"element": "Sign in", "ref": "e34"}
```
Either `element` (text/role description) or `ref` (from snapshot) must be provided.

**Output:** `{"clicked": "Sign in"}`

---

### 5. `browser.type`
Type text into a focused input field.

**Input:**
```json
{"element": "Search", "ref": "e5", "text": "golang tutorial", "clear": true}
```
`clear: true` clears the field before typing.

**Output:** `{"typed": "golang tutorial"}`

---

### 6. `browser.fill`
Fill multiple form fields at once (more efficient than repeated `browser.type` calls).

**Input:**
```json
{"fields": [
  {"element": "Username", "ref": "e1", "value": "alice"},
  {"element": "Password", "ref": "e2", "value": "secret"}
]}
```

**Output:** `{"filled": 2}`

---

### 7. `browser.select`
Select an option in a `<select>` dropdown.

**Input:**
```json
{"element": "Country", "ref": "e8", "value": "Ukraine"}
```

**Output:** `{"selected": "Ukraine"}`

---

### 8. `browser.eval`
Execute a JavaScript expression in the page context and return the result. Requires `browser.allow_eval: true` in config.

**Input:**
```json
{"expression": "document.title"}
```

**Output:** `{"result": "Example Domain"}`

---

### 9. `browser.wait`
Wait for a condition: URL match, CSS selector to appear, or text to appear on page.

**Input:**
```json
{"url": "https://example.com/dashboard", "timeout_ms": 10000}
```
or
```json
{"selector": ".success-message", "timeout_ms": 5000}
```
or
```json
{"text": "Welcome back", "timeout_ms": 5000}
```

**Output:** `{"matched": true, "elapsed_ms": 1234}`

---

### 10. `browser.close`
Close the current page (browser process stays alive for reuse).

**Input:** `{}`

**Output:** `{"closed": true}`

---

## BrowserClient Implementation

```go
// internal/browser/client.go
package browser

type Client struct {
    cfg    Config
    cmd    *exec.Cmd
    enc    *json.Encoder   // writes to subprocess stdin
    dec    *json.Decoder   // reads from subprocess stdout
    mu     sync.Mutex
    nextID atomic.Int64
    ready  bool
}

type Config struct {
    Headless       bool
    TimeoutMS      int
    ViewportWidth  int
    ViewportHeight int
    AllowEval      bool
}

func New(cfg Config) *Client

// ensureStarted: lazy-init — spawns npx subprocess + MCP initialize handshake.
// Called inside mu lock. Safe to call multiple times (no-op if ready).
func (c *Client) ensureStarted(ctx context.Context) error

// Call: make one MCP tools/call request and return the result content as JSON.
func (c *Client) Call(ctx context.Context, toolName string, args any) (json.RawMessage, error)

func (c *Client) Close() error
```

## Testing Strategy

### Unit tests (no real Node.js required)

`internal/browser/client_test.go` uses a mock MCP server (a Go subprocess that speaks the protocol):

```go
func TestClient_Navigate(t *testing.T) { ... }
func TestClient_FailsIfNpxMissing(t *testing.T) { ... }
func TestClient_RestartsAfterSubprocessCrash(t *testing.T) { ... }
func TestClient_TimeoutReturnsError(t *testing.T) { ... }
```

`internal/tools/browser_test.go`:

```go
func skipIfNoBrowser(t *testing.T)  // skips if npx not on PATH
func TestBrowserEval_RequiresAllowEval(t *testing.T)
func TestBrowserNavigate_RejectsEmptyURL(t *testing.T)
func TestBrowserWait_InvalidConditionFails(t *testing.T)
```

### Integration tests (real browser, gated by env var)

```go
// ORCH_E2E_BROWSER=1 go test ./internal/tools/ -run TestBrowserE2E -v
func TestBrowserE2E_NavigateAndSnapshot(t *testing.T) { ... }
func TestBrowserE2E_FillFormAndSubmit(t *testing.T) { ... }
func TestBrowserE2E_Screenshot(t *testing.T) { ... }
```

## Typical Agent Workflow

```
1. browser.navigate {"url": "http://localhost:3000"}
2. browser.snapshot {}
   → "- heading \"Login\" - textbox \"Email\" [ref=e1] - textbox \"Password\" [ref=e2] - button \"Sign In\" [ref=e3]"
3. browser.fill {"fields": [{"ref":"e1","value":"test@example.com"},{"ref":"e2","value":"pass"}]}
4. browser.click {"element": "Sign In", "ref": "e3"}
5. browser.wait {"url": "http://localhost:3000/dashboard"}
6. browser.screenshot {}  → verify visually
```

## External Dependency

`npx @playwright/mcp@latest` — requires Node.js ≥ 18 on the host machine. Playwright downloads Chromium on first run (~150MB). No new Go modules added.

## ToolsVersion

Bump `ToolsVersion` from 7 to 8 when this plan is implemented.
