# Browser Tools Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add 10 browser automation tools to the Orchestra agent via the official Playwright MCP server (`@playwright/mcp`), gated behind a new `--allow-browser` flag.

**Architecture:** A dedicated `internal/browser.Client` manages an `npx @playwright/mcp` subprocess over stdio using the MCP JSON-RPC protocol. The `tools.Runner` holds a `*browser.Client` (lazy-initialized on first use). All 10 browser tools are registered in registry.go and routed through call.go, following the same patterns as git.* and fs.* tools.

**Tech Stack:** Go stdlib (`os/exec`, `encoding/json`, `sync`), MCP JSON-RPC 2.0 protocol, `npx @playwright/mcp@latest` (Node.js, no new Go modules).

---

## File Map

| File | Change |
|---|---|
| `internal/browser/client.go` | **Create**: MCP subprocess client |
| `internal/browser/client_test.go` | **Create**: unit tests with mock subprocess |
| `internal/tools/browser.go` | **Create**: 10 browser tool implementations |
| `internal/tools/browser_test.go` | **Create**: unit + integration tests |
| `internal/config/config.go` | **Modify**: add `BrowserConfig` struct + `Browser BrowserConfig` field + browser names in `validAgentToolNames` |
| `internal/tools/runner.go` | **Modify**: add `browserClient *browser.Client` + `allowBrowserEval bool` fields; init in `NewRunner`; close in `Close()` |
| `internal/tools/registry.go` | **Modify**: add `allowBrowser bool` to ALL list variants + 10 tool defs + `allToolDefsMap` + `applyParallelFlags` |
| `internal/tools/call.go` | **Modify**: add 10 dispatch cases |
| `internal/tools/aliases_test.go` | **Modify**: add browser gating test |
| `internal/cli/apply.go` | **Modify**: add `--allow-browser` flag |
| `internal/protocol/version.go` | **Modify**: bump `ToolsVersion` 7 → 8 |

---

## Task 1: BrowserConfig in config.go

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Add `BrowserConfig` struct after `WebConfig`**

In `internal/config/config.go`, add this struct after the `WebConfig` block (around line 174):

```go
// BrowserConfig contains Playwright browser automation settings.
type BrowserConfig struct {
	Headless       bool `yaml:"headless"`
	TimeoutMS      int  `yaml:"timeout_ms"`
	ViewportWidth  int  `yaml:"viewport_width"`
	ViewportHeight int  `yaml:"viewport_height"`
	AllowEval      bool `yaml:"allow_eval"`
}
```

- [ ] **Step 2: Add `Browser BrowserConfig` field to `ProjectConfig`**

In `internal/config/config.go`, add the field to the `ProjectConfig` struct (after `Web WebConfig`):

```go
Browser     BrowserConfig     `yaml:"browser"`
```

- [ ] **Step 3: Add defaults in `DefaultConfig`**

In `DefaultConfig()`, after the `Web` field initialization, add:

```go
Browser: BrowserConfig{
    Headless:       true,
    TimeoutMS:      30000,
    ViewportWidth:  1280,
    ViewportHeight: 720,
    AllowEval:      false,
},
```

- [ ] **Step 4: Add browser tool names to `validAgentToolNames`**

In `validAgentToolNames` map (around line 230), add:

```go
"browser.navigate": true, "browser.snapshot": true, "browser.screenshot": true,
"browser.click": true, "browser.type": true, "browser.fill": true,
"browser.select": true, "browser.eval": true, "browser.wait": true, "browser.close": true,
```

- [ ] **Step 5: Build and verify**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(browser): add BrowserConfig to config"
```

---

## Task 2: internal/browser/client.go

**Files:**
- Create: `internal/browser/client.go`

- [ ] **Step 1: Create `internal/browser/` directory and `client.go`**

```go
// internal/browser/client.go
package browser

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/orchestra/orchestra/internal/protocol"
)

const npxInstallHint = "browser tools require Node.js and npx; install from https://nodejs.org"

// Config configures the browser MCP subprocess.
type Config struct {
	Headless       bool
	TimeoutMS      int
	ViewportWidth  int
	ViewportHeight int
	AllowEval      bool
	// CmdOverride replaces "npx @playwright/mcp" — for tests only.
	CmdOverride []string
}

// MCPContent is one item in an MCP tool result's content array.
type MCPContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// MCPResult is the parsed result of an MCP tools/call request.
type MCPResult struct {
	Content []MCPContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

// TextContent joins all text items from the result into one string.
func (r *MCPResult) TextContent() string {
	if r == nil {
		return ""
	}
	var parts []string
	for _, c := range r.Content {
		if c.Type == "text" && c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// ImageContent returns the base64 data of the first image item, or "".
func (r *MCPResult) ImageContent() string {
	if r == nil {
		return ""
	}
	for _, c := range r.Content {
		if c.Type == "image" {
			return c.Data
		}
	}
	return ""
}

// Client manages the Playwright MCP subprocess and its JSON-RPC communication.
type Client struct {
	cfg    Config
	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	enc    *json.Encoder
	dec    *json.Decoder
	nextID atomic.Int64
	ready  bool
}

// New creates a new Client. The subprocess is not started until the first Call.
func New(cfg Config) *Client {
	if cfg.TimeoutMS <= 0 {
		cfg.TimeoutMS = 30000
	}
	if cfg.ViewportWidth <= 0 {
		cfg.ViewportWidth = 1280
	}
	if cfg.ViewportHeight <= 0 {
		cfg.ViewportHeight = 720
	}
	return &Client{cfg: cfg}
}

type mcpRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *Client) makeCmd() *exec.Cmd {
	if len(c.cfg.CmdOverride) > 0 {
		if len(c.cfg.CmdOverride) == 1 {
			return exec.Command(c.cfg.CmdOverride[0])
		}
		return exec.Command(c.cfg.CmdOverride[0], c.cfg.CmdOverride[1:]...)
	}
	args := []string{"--yes", "@playwright/mcp@latest"}
	if c.cfg.Headless {
		args = append(args, "--headless")
	}
	args = append(args, "--viewport-size",
		fmt.Sprintf("%d,%d", c.cfg.ViewportWidth, c.cfg.ViewportHeight))
	return exec.Command("npx", args...)
}

// ensureStarted starts the subprocess and performs the MCP initialize handshake.
// Must be called with c.mu held.
func (c *Client) ensureStarted(ctx context.Context) error {
	if c.ready {
		return nil
	}
	cmd := c.makeCmd()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return protocol.NewError(protocol.ExecFailed, npxInstallHint, nil)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return protocol.NewError(protocol.ExecFailed, npxInstallHint, nil)
	}

	if startErr := cmd.Start(); startErr != nil {
		_ = stdin.Close()
		if errors.Is(startErr, exec.ErrNotFound) ||
			strings.Contains(startErr.Error(), "executable file not found") {
			return protocol.NewError(protocol.ExecFailed, npxInstallHint, nil)
		}
		return protocol.NewError(protocol.ExecFailed,
			fmt.Sprintf("start browser subprocess: %v", startErr), nil)
	}

	c.cmd = cmd
	c.stdin = stdin
	c.stdout = stdout
	c.enc = json.NewEncoder(stdin)
	c.dec = json.NewDecoder(bufio.NewReader(stdout))

	if err := c.handshake(ctx); err != nil {
		c.killSubprocess()
		return err
	}
	c.ready = true
	return nil
}

func (c *Client) handshake(ctx context.Context) error {
	timeout := time.Duration(c.cfg.TimeoutMS) * time.Millisecond
	type res struct {
		err error
	}
	ch := make(chan res, 1)

	go func() {
		id := c.nextID.Add(1)
		req := mcpRequest{
			JSONRPC: "2.0",
			ID:      id,
			Method:  "initialize",
			Params: map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{},
				"clientInfo":      map[string]any{"name": "orchestra", "version": "1"},
			},
		}
		if err := c.enc.Encode(req); err != nil {
			ch <- res{err: fmt.Errorf("encode initialize: %w", err)}
			return
		}
		var resp mcpResponse
		if err := c.dec.Decode(&resp); err != nil {
			ch <- res{err: fmt.Errorf("decode initialize response: %w", err)}
			return
		}
		if resp.Error != nil {
			ch <- res{err: fmt.Errorf("initialize error: %s", resp.Error.Message)}
			return
		}
		// Send notifications/initialized (no response expected).
		_ = c.enc.Encode(mcpRequest{JSONRPC: "2.0", Method: "notifications/initialized"})
		ch <- res{}
	}()

	select {
	case <-ctx.Done():
		return protocol.NewError(protocol.ExecTimeout, "browser initialize timed out", nil)
	case <-time.After(timeout):
		return protocol.NewError(protocol.ExecTimeout, "browser initialize timed out", nil)
	case r := <-ch:
		if r.err != nil {
			return protocol.NewError(protocol.ExecFailed,
				fmt.Sprintf("browser initialize: %v", r.err), nil)
		}
		return nil
	}
}

// Call makes one MCP tools/call request. On subprocess crash, retries once.
func (c *Client) Call(ctx context.Context, toolName string, args any) (*MCPResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.ensureStarted(ctx); err != nil {
		return nil, err
	}

	res, err := c.doCall(ctx, toolName, args)
	if err != nil && c.isSubprocessDead() {
		c.ready = false
		if startErr := c.ensureStarted(ctx); startErr != nil {
			return nil, err
		}
		res, err = c.doCall(ctx, toolName, args)
	}
	return res, err
}

func (c *Client) doCall(ctx context.Context, toolName string, args any) (*MCPResult, error) {
	id := c.nextID.Add(1)
	req := mcpRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "tools/call",
		Params:  map[string]any{"name": toolName, "arguments": args},
	}
	timeout := time.Duration(c.cfg.TimeoutMS) * time.Millisecond

	type res struct {
		result *MCPResult
		err    error
	}
	ch := make(chan res, 1)

	go func() {
		if err := c.enc.Encode(req); err != nil {
			ch <- res{err: protocol.NewError(protocol.ExecFailed,
				fmt.Sprintf("encode request: %v", err), nil)}
			return
		}
		var resp mcpResponse
		if err := c.dec.Decode(&resp); err != nil {
			ch <- res{err: protocol.NewError(protocol.ExecFailed,
				fmt.Sprintf("decode response: %v", err), nil)}
			return
		}
		if resp.Error != nil {
			ch <- res{err: protocol.NewError(protocol.ExecFailed, resp.Error.Message, nil)}
			return
		}
		var result MCPResult
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			ch <- res{err: protocol.NewError(protocol.ExecFailed,
				fmt.Sprintf("parse MCP result: %v", err), nil)}
			return
		}
		if result.IsError {
			msg := result.TextContent()
			if msg == "" {
				msg = "browser tool error"
			}
			ch <- res{err: protocol.NewError(protocol.ExecFailed, msg, nil)}
			return
		}
		ch <- res{result: &result}
	}()

	select {
	case <-ctx.Done():
		return nil, protocol.NewError(protocol.ExecTimeout, "browser operation timed out", nil)
	case <-time.After(timeout):
		return nil, protocol.NewError(protocol.ExecTimeout, "browser operation timed out", nil)
	case r := <-ch:
		return r.result, r.err
	}
}

func (c *Client) isSubprocessDead() bool {
	return c.cmd == nil || c.cmd.ProcessState != nil
}

func (c *Client) killSubprocess() {
	if c.stdin != nil {
		_ = c.stdin.Close()
		c.stdin = nil
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	}
	c.cmd = nil
	c.stdout = nil
	c.enc = nil
	c.dec = nil
}

// Close shuts down the subprocess. Safe to call multiple times.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.ready && c.cmd == nil {
		return nil
	}
	c.ready = false
	c.killSubprocess()
	return nil
}
```

- [ ] **Step 2: Build**

```bash
go build ./internal/browser/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/browser/client.go
git commit -m "feat(browser): add MCP subprocess client"
```

---

## Task 3: internal/browser/client_test.go

**Files:**
- Create: `internal/browser/client_test.go`

The tests use the test binary itself as the mock MCP server (standard Go subprocess testing pattern). When `BE_MOCK_MCP_SERVER=1` is set, the binary acts as the mock server.

- [ ] **Step 1: Create the test file**

```go
// internal/browser/client_test.go
package browser

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// TestMain branches: if BE_MOCK_MCP_SERVER=1, act as mock MCP server.
func TestMain(m *testing.M) {
	if os.Getenv("BE_MOCK_MCP_SERVER") == "1" {
		runMockMCPServer()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runMockMCPServer reads MCP JSON-RPC requests from stdin and writes responses to stdout.
// It handles: initialize, notifications/initialized, tools/call (browser_navigate, browser_snapshot).
func runMockMCPServer() {
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)

	for {
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      any             `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := dec.Decode(&req); err != nil {
			return
		}

		switch req.Method {
		case "initialize":
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": "playwright"},
				},
			})
		case "notifications/initialized":
			// no response
		case "tools/call":
			var params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &params)
			var text string
			switch params.Name {
			case "browser_navigate":
				text = "Navigated to https://example.com"
			case "browser_snapshot":
				text = "- heading \"Example Domain\" [level=1]"
			case "browser_close":
				text = "Browser closed"
			default:
				text = "ok"
			}
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"content": []map[string]any{
						{"type": "text", "text": text},
					},
				},
			})
		}
	}
}

// newMockClient creates a Client that uses the test binary as the MCP server.
func newMockClient(t *testing.T) *Client {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cfg := Config{
		Headless:       true,
		TimeoutMS:      5000,
		ViewportWidth:  1280,
		ViewportHeight: 720,
		CmdOverride:    []string{exe, "-test.run=^TestMain$", "-test.v=false"},
	}
	c := New(cfg)
	t.Setenv("BE_MOCK_MCP_SERVER", "1")
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestClient_Navigate(t *testing.T) {
	c := newMockClient(t)
	ctx := context.Background()

	res, err := c.Call(ctx, "browser_navigate", map[string]any{"url": "https://example.com"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	text := res.TextContent()
	if text == "" {
		t.Error("expected non-empty text content")
	}
}

func TestClient_Snapshot(t *testing.T) {
	c := newMockClient(t)
	ctx := context.Background()

	res, err := c.Call(ctx, "browser_snapshot", map[string]any{})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	text := res.TextContent()
	if text == "" {
		t.Error("expected non-empty snapshot")
	}
}

func TestClient_MultipleCallsReuseSubprocess(t *testing.T) {
	c := newMockClient(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := c.Call(ctx, "browser_navigate", map[string]any{"url": "https://example.com"}); err != nil {
			t.Fatalf("Call %d: %v", i, err)
		}
	}
}

func TestClient_TimeoutReturnsError(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	// Use a very short timeout so the initialize handshake times out.
	cfg := Config{
		Headless:    true,
		TimeoutMS:   1, // 1ms — will always time out
		CmdOverride: []string{exe, "-test.run=^TestMain$", "-test.v=false"},
	}
	t.Setenv("BE_MOCK_MCP_SERVER", "1")
	c := New(cfg)
	defer func() { _ = c.Close() }()

	ctx := context.Background()
	_, err = c.Call(ctx, "browser_navigate", map[string]any{"url": "https://example.com"})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestClient_FailsIfNpxMissing(t *testing.T) {
	cfg := Config{
		Headless:    true,
		TimeoutMS:   5000,
		CmdOverride: []string{"/nonexistent/binary/that/does/not/exist"},
	}
	c := New(cfg)
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := c.Call(ctx, "browser_navigate", map[string]any{"url": "https://example.com"})
	if err == nil {
		t.Fatal("expected error for missing binary, got nil")
	}
}

func TestClient_Close_Idempotent(t *testing.T) {
	c := newMockClient(t)
	ctx := context.Background()

	if _, err := c.Call(ctx, "browser_navigate", map[string]any{"url": "https://example.com"}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}
```

- [ ] **Step 2: Run tests**

```bash
go test ./internal/browser/... -v -timeout 30s
```

Expected: all 5 tests pass. (TestClient_TimeoutReturnsError may be flaky on slow CI — if so, increase the timeout to 10ms.)

- [ ] **Step 3: Commit**

```bash
git add internal/browser/client_test.go
git commit -m "test(browser): add client unit tests with mock MCP subprocess"
```

---

## Task 4: internal/tools/runner.go — browser integration

**Files:**
- Modify: `internal/tools/runner.go`

- [ ] **Step 1: Add import for `internal/browser`**

Add to the import block in `runner.go`:

```go
"github.com/orchestra/orchestra/internal/browser"
```

- [ ] **Step 2: Add fields to `Runner`**

Add after `lspManager *lsp.Manager`:

```go
browserClient    *browser.Client
allowBrowserEval bool
```

- [ ] **Step 3: Add fields to `RunnerOptions`**

Add after the `LSP config.LSPConfig` field:

```go
Browser       config.BrowserConfig
AllowBrowser  bool
```

- [ ] **Step 4: Initialize browser client in `NewRunner`**

Add after the `lspMgr` initialization (before the `return` statement):

```go
var browserCli *browser.Client
if opts.AllowBrowser {
    browserCli = browser.New(browser.Config{
        Headless:       opts.Browser.Headless,
        TimeoutMS:      opts.Browser.TimeoutMS,
        ViewportWidth:  opts.Browser.ViewportWidth,
        ViewportHeight: opts.Browser.ViewportHeight,
        AllowEval:      opts.Browser.AllowEval,
    })
}
```

Update the return statement to include the new fields:

```go
return &Runner{
    workspaceRoot:      rootAbs,
    excludeDirs:        exclude,
    execTimeout:        timeout,
    execOutputLimit:    limit,
    ckgStore:           store,
    ckgProvider:        provider,
    webFetchTimeout:    webTimeout,
    webMaxContentBytes: webMaxBytes,
    lspManager:         lspMgr,
    browserClient:      browserCli,
    allowBrowserEval:   opts.Browser.AllowEval,
    dryRun:             opts.DryRun,
    staged:             make(map[string]*stagedFile),
}, nil
```

- [ ] **Step 5: Close browser client in `Close()`**

Add at the top of `Close()`, before the lspManager check:

```go
if r.browserClient != nil {
    _ = r.browserClient.Close()
    r.browserClient = nil
}
```

- [ ] **Step 6: Build and verify**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add internal/tools/runner.go
git commit -m "feat(browser): integrate browser.Client into tools.Runner"
```

---

## Task 5: internal/tools/browser.go — 10 tool implementations

**Files:**
- Create: `internal/tools/browser.go`

- [ ] **Step 1: Create `internal/tools/browser.go`**

```go
// internal/tools/browser.go
package tools

import (
	"context"
	"strings"

	"github.com/orchestra/orchestra/internal/browser"
	"github.com/orchestra/orchestra/internal/protocol"
)

// errNoBrowser is returned when browser tools are called without --allow-browser.
func errNoBrowser() error {
	return protocol.NewError(protocol.ExecDenied,
		"browser tools require --allow-browser flag", nil)
}

// --- browser.navigate ---

type BrowserNavigateRequest struct {
	URL       string `json:"url"`
	WaitUntil string `json:"wait_until,omitempty"`
}

type BrowserNavigateResponse struct {
	Result string `json:"result"`
}

func (r *Runner) BrowserNavigate(ctx context.Context, req BrowserNavigateRequest) (*BrowserNavigateResponse, error) {
	if r.browserClient == nil {
		return nil, errNoBrowser()
	}
	if strings.TrimSpace(req.URL) == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "url is required", nil)
	}
	args := map[string]any{"url": req.URL}
	if req.WaitUntil != "" {
		args["waitUntil"] = req.WaitUntil
	}
	res, err := r.browserClient.Call(ctx, "browser_navigate", args)
	if err != nil {
		return nil, err
	}
	return &BrowserNavigateResponse{Result: res.TextContent()}, nil
}

// --- browser.snapshot ---

type BrowserSnapshotRequest struct{}

type BrowserSnapshotResponse struct {
	Snapshot string `json:"snapshot"`
}

func (r *Runner) BrowserSnapshot(ctx context.Context, req BrowserSnapshotRequest) (*BrowserSnapshotResponse, error) {
	if r.browserClient == nil {
		return nil, errNoBrowser()
	}
	res, err := r.browserClient.Call(ctx, "browser_snapshot", map[string]any{})
	if err != nil {
		return nil, err
	}
	return &BrowserSnapshotResponse{Snapshot: res.TextContent()}, nil
}

// --- browser.screenshot ---

type BrowserScreenshotRequest struct {
	FullPage bool `json:"full_page,omitempty"`
}

type BrowserScreenshotResponse struct {
	Image string `json:"image"` // base64 PNG, or text if no image
}

func (r *Runner) BrowserScreenshot(ctx context.Context, req BrowserScreenshotRequest) (*BrowserScreenshotResponse, error) {
	if r.browserClient == nil {
		return nil, errNoBrowser()
	}
	res, err := r.browserClient.Call(ctx, "browser_take_screenshot", map[string]any{
		"fullPage": req.FullPage,
	})
	if err != nil {
		return nil, err
	}
	img := res.ImageContent()
	if img == "" {
		img = res.TextContent() // fallback
	}
	return &BrowserScreenshotResponse{Image: img}, nil
}

// --- browser.click ---

type BrowserClickRequest struct {
	Element string `json:"element,omitempty"`
	Ref     string `json:"ref,omitempty"`
}

type BrowserClickResponse struct {
	Result string `json:"result"`
}

func (r *Runner) BrowserClick(ctx context.Context, req BrowserClickRequest) (*BrowserClickResponse, error) {
	if r.browserClient == nil {
		return nil, errNoBrowser()
	}
	if req.Element == "" && req.Ref == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "element or ref is required", nil)
	}
	args := map[string]any{}
	if req.Element != "" {
		args["element"] = req.Element
	}
	if req.Ref != "" {
		args["ref"] = req.Ref
	}
	res, err := r.browserClient.Call(ctx, "browser_click", args)
	if err != nil {
		return nil, err
	}
	return &BrowserClickResponse{Result: res.TextContent()}, nil
}

// --- browser.type ---

type BrowserTypeRequest struct {
	Element string `json:"element,omitempty"`
	Ref     string `json:"ref,omitempty"`
	Text    string `json:"text"`
	Clear   bool   `json:"clear,omitempty"`
}

type BrowserTypeResponse struct {
	Result string `json:"result"`
}

func (r *Runner) BrowserType(ctx context.Context, req BrowserTypeRequest) (*BrowserTypeResponse, error) {
	if r.browserClient == nil {
		return nil, errNoBrowser()
	}
	if req.Text == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "text is required", nil)
	}
	if req.Element == "" && req.Ref == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "element or ref is required", nil)
	}
	args := map[string]any{"text": req.Text}
	if req.Element != "" {
		args["element"] = req.Element
	}
	if req.Ref != "" {
		args["ref"] = req.Ref
	}
	if req.Clear {
		args["slowly"] = false
		args["clear"] = true
	}
	res, err := r.browserClient.Call(ctx, "browser_type", args)
	if err != nil {
		return nil, err
	}
	return &BrowserTypeResponse{Result: res.TextContent()}, nil
}

// --- browser.fill ---

type BrowserFillField struct {
	Element string `json:"element,omitempty"`
	Ref     string `json:"ref,omitempty"`
	Value   string `json:"value"`
}

type BrowserFillRequest struct {
	Fields []BrowserFillField `json:"fields"`
}

type BrowserFillResponse struct {
	Filled int `json:"filled"`
}

func (r *Runner) BrowserFill(ctx context.Context, req BrowserFillRequest) (*BrowserFillResponse, error) {
	if r.browserClient == nil {
		return nil, errNoBrowser()
	}
	if len(req.Fields) == 0 {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "fields array is required and must not be empty", nil)
	}
	// The Playwright MCP browser_fill_form tool accepts a list of form fields.
	// Convert our fields to the MCP format.
	mcpFields := make([]map[string]any, 0, len(req.Fields))
	for _, f := range req.Fields {
		mf := map[string]any{"value": f.Value}
		if f.Ref != "" {
			mf["ref"] = f.Ref
		}
		if f.Element != "" {
			mf["element"] = f.Element
		}
		mcpFields = append(mcpFields, mf)
	}
	_, err := r.browserClient.Call(ctx, "browser_fill_form", map[string]any{"form": mcpFields})
	if err != nil {
		return nil, err
	}
	return &BrowserFillResponse{Filled: len(req.Fields)}, nil
}

// --- browser.select ---

type BrowserSelectRequest struct {
	Element string `json:"element,omitempty"`
	Ref     string `json:"ref,omitempty"`
	Value   string `json:"value"`
}

type BrowserSelectResponse struct {
	Result string `json:"result"`
}

func (r *Runner) BrowserSelect(ctx context.Context, req BrowserSelectRequest) (*BrowserSelectResponse, error) {
	if r.browserClient == nil {
		return nil, errNoBrowser()
	}
	if req.Element == "" && req.Ref == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "element or ref is required", nil)
	}
	args := map[string]any{"values": []string{req.Value}}
	if req.Element != "" {
		args["element"] = req.Element
	}
	if req.Ref != "" {
		args["ref"] = req.Ref
	}
	res, err := r.browserClient.Call(ctx, "browser_select_option", args)
	if err != nil {
		return nil, err
	}
	return &BrowserSelectResponse{Result: res.TextContent()}, nil
}

// --- browser.eval ---

type BrowserEvalRequest struct {
	Expression string `json:"expression"`
}

type BrowserEvalResponse struct {
	Result string `json:"result"`
}

func (r *Runner) BrowserEval(ctx context.Context, req BrowserEvalRequest) (*BrowserEvalResponse, error) {
	if r.browserClient == nil {
		return nil, errNoBrowser()
	}
	if !r.allowBrowserEval {
		return nil, protocol.NewError(protocol.ExecDenied,
			"browser.eval requires allow_eval: true in browser config", nil)
	}
	if strings.TrimSpace(req.Expression) == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "expression is required", nil)
	}
	res, err := r.browserClient.Call(ctx, "browser_evaluate", map[string]any{
		"expression": req.Expression,
	})
	if err != nil {
		return nil, err
	}
	return &BrowserEvalResponse{Result: res.TextContent()}, nil
}

// --- browser.wait ---

type BrowserWaitRequest struct {
	URL       string `json:"url,omitempty"`
	Selector  string `json:"selector,omitempty"`
	Text      string `json:"text,omitempty"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
}

type BrowserWaitResponse struct {
	Result string `json:"result"`
}

func (r *Runner) BrowserWait(ctx context.Context, req BrowserWaitRequest) (*BrowserWaitResponse, error) {
	if r.browserClient == nil {
		return nil, errNoBrowser()
	}
	if req.URL == "" && req.Selector == "" && req.Text == "" {
		return nil, protocol.NewError(protocol.InvalidLLMOutput,
			"one of url, selector, or text is required", nil)
	}
	args := map[string]any{}
	if req.URL != "" {
		args["url"] = req.URL
	}
	if req.Selector != "" {
		args["selector"] = req.Selector
	}
	if req.Text != "" {
		args["text"] = req.Text
	}
	if req.TimeoutMS > 0 {
		args["timeout"] = req.TimeoutMS
	}
	res, err := r.browserClient.Call(ctx, "browser_wait_for", args)
	if err != nil {
		return nil, err
	}
	return &BrowserWaitResponse{Result: res.TextContent()}, nil
}

// --- browser.close ---

type BrowserCloseRequest struct{}

type BrowserCloseResponse struct {
	Closed bool `json:"closed"`
}

func (r *Runner) BrowserClose(ctx context.Context, req BrowserCloseRequest) (*BrowserCloseResponse, error) {
	if r.browserClient == nil {
		return nil, errNoBrowser()
	}
	_, err := r.browserClient.Call(ctx, "browser_close", map[string]any{})
	if err != nil {
		return nil, err
	}
	return &BrowserCloseResponse{Closed: true}, nil
}
```

- [ ] **Step 2: Build**

```bash
go build ./internal/tools/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/tools/browser.go
git commit -m "feat(browser): add 10 browser tool implementations"
```

---

## Task 6: internal/tools/browser_test.go — tool tests

**Files:**
- Create: `internal/tools/browser_test.go`

- [ ] **Step 1: Create test file**

```go
// internal/tools/browser_test.go
package tools

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/orchestra/orchestra/internal/browser"
	"github.com/orchestra/orchestra/internal/config"
)

// skipIfNoBrowser skips the test if npx is not available (for integration tests).
func skipIfNoBrowser(t *testing.T) {
	t.Helper()
	if os.Getenv("ORCH_E2E_BROWSER") != "1" {
		t.Skip("set ORCH_E2E_BROWSER=1 to run browser integration tests")
	}
}

// newBrowserRunner creates a Runner backed by a mock browser.Client.
// The mock acts as the MCP server using the test binary subprocess pattern.
func newBrowserRunner(t *testing.T, allowEval bool) *Runner {
	t.Helper()
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cfg := browser.Config{
		Headless:    true,
		TimeoutMS:   5000,
		AllowEval:   allowEval,
		CmdOverride: []string{exe, "-test.run=^TestBrowserMain$", "-test.v=false"},
	}
	t.Setenv("BE_BROWSER_MOCK", "1")
	r.browserClient = browser.New(cfg)
	r.allowBrowserEval = allowEval
	return r
}

// TestBrowserMain is invoked as a subprocess to act as a mock MCP server.
func TestBrowserMain(t *testing.T) {
	if os.Getenv("BE_BROWSER_MOCK") != "1" {
		t.Skip()
		return
	}
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for {
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      any             `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := dec.Decode(&req); err != nil {
			return
		}
		switch req.Method {
		case "initialize":
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0", "id": req.ID,
				"result": map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": "playwright"},
				},
			})
		case "notifications/initialized":
		case "tools/call":
			var p struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(req.Params, &p)
			text := "ok:" + p.Name
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0", "id": req.ID,
				"result": map[string]any{
					"content": []map[string]any{{"type": "text", "text": text}},
				},
			})
		}
	}
}

func TestBrowserNavigate_RejectsEmptyURL(t *testing.T) {
	r := newBrowserRunner(t, false)
	ctx := context.Background()
	_, err := r.BrowserNavigate(ctx, BrowserNavigateRequest{URL: ""})
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestBrowserNavigate_DisabledWithoutFlag(t *testing.T) {
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer func() { _ = r.Close() }()
	// browserClient is nil — simulates --allow-browser not set
	_, err = r.BrowserNavigate(context.Background(), BrowserNavigateRequest{URL: "https://example.com"})
	if err == nil {
		t.Fatal("expected error when browserClient is nil")
	}
}

func TestBrowserEval_RequiresAllowEval(t *testing.T) {
	r := newBrowserRunner(t, false) // allowEval=false
	ctx := context.Background()
	_, err := r.BrowserEval(ctx, BrowserEvalRequest{Expression: "document.title"})
	if err == nil {
		t.Fatal("expected error when allowEval=false")
	}
}

func TestBrowserEval_WorksWhenAllowed(t *testing.T) {
	r := newBrowserRunner(t, true) // allowEval=true
	ctx := context.Background()
	res, err := r.BrowserEval(ctx, BrowserEvalRequest{Expression: "document.title"})
	if err != nil {
		t.Fatalf("BrowserEval: %v", err)
	}
	if res.Result == "" {
		t.Error("expected non-empty result")
	}
}

func TestBrowserClick_RequiresElementOrRef(t *testing.T) {
	r := newBrowserRunner(t, false)
	ctx := context.Background()
	_, err := r.BrowserClick(ctx, BrowserClickRequest{})
	if err == nil {
		t.Fatal("expected error when both element and ref are empty")
	}
}

func TestBrowserWait_RequiresCondition(t *testing.T) {
	r := newBrowserRunner(t, false)
	ctx := context.Background()
	_, err := r.BrowserWait(ctx, BrowserWaitRequest{})
	if err == nil {
		t.Fatal("expected error when no condition provided")
	}
}

func TestBrowserFill_RejectsEmptyFields(t *testing.T) {
	r := newBrowserRunner(t, false)
	ctx := context.Background()
	_, err := r.BrowserFill(ctx, BrowserFillRequest{Fields: nil})
	if err == nil {
		t.Fatal("expected error for empty fields")
	}
}

func TestBrowserNavigate_CallsClient(t *testing.T) {
	r := newBrowserRunner(t, false)
	ctx := context.Background()
	res, err := r.BrowserNavigate(ctx, BrowserNavigateRequest{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("BrowserNavigate: %v", err)
	}
	if res.Result == "" {
		t.Error("expected non-empty result")
	}
}

func TestBrowserSnapshot_CallsClient(t *testing.T) {
	r := newBrowserRunner(t, false)
	ctx := context.Background()
	res, err := r.BrowserSnapshot(ctx, BrowserSnapshotRequest{})
	if err != nil {
		t.Fatalf("BrowserSnapshot: %v", err)
	}
	if res.Snapshot == "" {
		t.Error("expected non-empty snapshot")
	}
}

// --- Integration tests (real browser, gated by ORCH_E2E_BROWSER=1) ---

func TestBrowserE2E_NavigateAndSnapshot(t *testing.T) {
	skipIfNoBrowser(t)
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{
		Browser:     config.BrowserConfig{Headless: true, TimeoutMS: 30000, ViewportWidth: 1280, ViewportHeight: 720},
		AllowBrowser: true,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer func() { _ = r.Close() }()

	ctx := context.Background()
	navRes, err := r.BrowserNavigate(ctx, BrowserNavigateRequest{URL: "https://example.com"})
	if err != nil {
		t.Fatalf("BrowserNavigate: %v", err)
	}
	t.Logf("navigate result: %s", navRes.Result)

	snapRes, err := r.BrowserSnapshot(ctx, BrowserSnapshotRequest{})
	if err != nil {
		t.Fatalf("BrowserSnapshot: %v", err)
	}
	if snapRes.Snapshot == "" {
		t.Error("expected non-empty snapshot")
	}
	t.Logf("snapshot: %s", snapRes.Snapshot[:min(200, len(snapRes.Snapshot))])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
```

- [ ] **Step 2: Run unit tests (no browser required)**

```bash
go test ./internal/tools/... -run TestBrowser -v -timeout 30s
```

Expected: all non-E2E browser tests pass (8 unit tests).

- [ ] **Step 3: Commit**

```bash
git add internal/tools/browser_test.go
git commit -m "test(browser): add browser tool unit + integration tests"
```

---

## Task 7: registry.go — add allowBrowser to all list variants

**Files:**
- Modify: `internal/tools/registry.go`

This is the most critical task. There are **7 functions** that need `allowBrowser bool` added, plus `allToolDefsMap` and `applyParallelFlags` must be updated.

- [ ] **Step 1: Update `ListTools` signature and body**

Change:
```go
func ListTools(allowExec, allowWeb bool) []llm.ToolDef {
```
To:
```go
func ListTools(allowExec, allowWeb, allowBrowser bool) []llm.ToolDef {
```

Add after `if allowWeb { ... }`:
```go
if allowBrowser {
    out = append(out,
        toolBrowserNavigate(), toolBrowserSnapshot(), toolBrowserScreenshot(),
        toolBrowserClick(), toolBrowserType(), toolBrowserFill(),
        toolBrowserSelect(), toolBrowserEval(), toolBrowserWait(), toolBrowserClose(),
    )
}
```

- [ ] **Step 2: Update `ListToolsWithMCP`**

Change:
```go
func ListToolsWithMCP(allowExec, allowWeb bool, mcpDefs []llm.ToolDef) []llm.ToolDef {
    out := ListTools(allowExec, allowWeb)
```
To:
```go
func ListToolsWithMCP(allowExec, allowWeb, allowBrowser bool, mcpDefs []llm.ToolDef) []llm.ToolDef {
    out := ListTools(allowExec, allowWeb, allowBrowser)
```

- [ ] **Step 3: Update `ListToolsWithSubtasksAndMCP`**

Change:
```go
func ListToolsWithSubtasksAndMCP(allowExec, allowWeb bool, mcpDefs []llm.ToolDef) []llm.ToolDef {
    out := ListToolsWithSubtasks(allowExec, allowWeb)
```
To:
```go
func ListToolsWithSubtasksAndMCP(allowExec, allowWeb, allowBrowser bool, mcpDefs []llm.ToolDef) []llm.ToolDef {
    out := ListToolsWithSubtasks(allowExec, allowWeb, allowBrowser)
```

- [ ] **Step 4: Update `ListToolsWithSubtasks`**

Change:
```go
func ListToolsWithSubtasks(allowExec, allowWeb bool) []llm.ToolDef {
    out := ListTools(allowExec, allowWeb)
```
To:
```go
func ListToolsWithSubtasks(allowExec, allowWeb, allowBrowser bool) []llm.ToolDef {
    out := ListTools(allowExec, allowWeb, allowBrowser)
```

- [ ] **Step 5: Update `ListToolsForMode`**

Change:
```go
func ListToolsForMode(mode string, allowExec, allowWeb, hasSubtasks, hasQuestionAsker bool) []llm.ToolDef {
    switch mode {
    ...
    case "general":
        return listToolsGeneral(allowExec, allowWeb, hasSubtasks)
    ...
    default: // "build" or ""
        return listToolsBuild(allowExec, allowWeb, hasSubtasks, hasQuestionAsker)
    }
}
```
To:
```go
func ListToolsForMode(mode string, allowExec, allowWeb, allowBrowser, hasSubtasks, hasQuestionAsker bool) []llm.ToolDef {
    switch mode {
    ...
    case "general":
        return listToolsGeneral(allowExec, allowWeb, allowBrowser, hasSubtasks)
    ...
    default: // "build" or ""
        return listToolsBuild(allowExec, allowWeb, allowBrowser, hasSubtasks, hasQuestionAsker)
    }
}
```

(Keep `case "plan"`, `case "explore"`, `case "compaction"/"title"/"summary"` unchanged — they don't get browser tools.)

- [ ] **Step 6: Update `listToolsBuild`**

Change:
```go
func listToolsBuild(allowExec, allowWeb, hasSubtasks, hasQuestionAsker bool) []llm.ToolDef {
```
To:
```go
func listToolsBuild(allowExec, allowWeb, allowBrowser, hasSubtasks, hasQuestionAsker bool) []llm.ToolDef {
```

Add after `if allowWeb { ... }`:
```go
if allowBrowser {
    out = append(out,
        toolBrowserNavigate(), toolBrowserSnapshot(), toolBrowserScreenshot(),
        toolBrowserClick(), toolBrowserType(), toolBrowserFill(),
        toolBrowserSelect(), toolBrowserEval(), toolBrowserWait(), toolBrowserClose(),
    )
}
```

- [ ] **Step 7: Update `listToolsGeneral`**

Change:
```go
func listToolsGeneral(allowExec, allowWeb, hasSubtasks bool) []llm.ToolDef {
```
To:
```go
func listToolsGeneral(allowExec, allowWeb, allowBrowser, hasSubtasks bool) []llm.ToolDef {
```

Add after `if allowWeb { ... }`:
```go
if allowBrowser {
    out = append(out,
        toolBrowserNavigate(), toolBrowserSnapshot(), toolBrowserScreenshot(),
        toolBrowserClick(), toolBrowserType(), toolBrowserFill(),
        toolBrowserSelect(), toolBrowserEval(), toolBrowserWait(), toolBrowserClose(),
    )
}
```

- [ ] **Step 8: Update `allToolDefsMap`**

Add to the `all` slice:
```go
toolBrowserNavigate(), toolBrowserSnapshot(), toolBrowserScreenshot(),
toolBrowserClick(), toolBrowserType(), toolBrowserFill(),
toolBrowserSelect(), toolBrowserEval(), toolBrowserWait(), toolBrowserClose(),
```

- [ ] **Step 9: Update `applyParallelFlags`**

Add to the `ParallelSafe` case list:
```go
"browser.snapshot", "browser.screenshot":
```

Add to the `Mutating` case list:
```go
"browser.navigate", "browser.click", "browser.type", "browser.fill",
"browser.select", "browser.eval", "browser.wait", "browser.close":
```

- [ ] **Step 10: Add 10 tool definition functions at the bottom of registry.go**

```go
func toolBrowserNavigate() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.navigate",
			Description: "Открыть URL в браузере и дождаться загрузки страницы.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["url"],
  "properties": {
    "url": { "type": "string", "minLength": 1 },
    "wait_until": { "type": "string", "enum": ["load", "domcontentloaded", "networkidle"] }
  }
}`),
		},
	}
}

func toolBrowserSnapshot() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.snapshot",
			Description: "Вернуть accessibility-дерево текущей страницы (структурированный текст с ref-идентификаторами для кликов).",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {}
}`),
		},
	}
}

func toolBrowserScreenshot() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.screenshot",
			Description: "Снять скриншот текущей страницы (base64 PNG).",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "full_page": { "type": "boolean" }
  }
}`),
		},
	}
}

func toolBrowserClick() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.click",
			Description: "Нажать на элемент по имени или ref из snapshot.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "element": { "type": "string" },
    "ref": { "type": "string" }
  }
}`),
		},
	}
}

func toolBrowserType() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.type",
			Description: "Ввести текст в поле ввода (по имени или ref из snapshot).",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["text"],
  "properties": {
    "element": { "type": "string" },
    "ref": { "type": "string" },
    "text": { "type": "string" },
    "clear": { "type": "boolean" }
  }
}`),
		},
	}
}

func toolBrowserFill() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.fill",
			Description: "Заполнить несколько полей формы за один вызов.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["fields"],
  "properties": {
    "fields": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["value"],
        "properties": {
          "element": { "type": "string" },
          "ref": { "type": "string" },
          "value": { "type": "string" }
        }
      }
    }
  }
}`),
		},
	}
}

func toolBrowserSelect() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.select",
			Description: "Выбрать опцию в выпадающем списке <select>.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["value"],
  "properties": {
    "element": { "type": "string" },
    "ref": { "type": "string" },
    "value": { "type": "string" }
  }
}`),
		},
	}
}

func toolBrowserEval() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.eval",
			Description: "Выполнить JavaScript в контексте страницы. Требует allow_eval: true в конфигурации.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["expression"],
  "properties": {
    "expression": { "type": "string", "minLength": 1 }
  }
}`),
		},
	}
}

func toolBrowserWait() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.wait",
			Description: "Ждать условие: совпадение URL, появление CSS-селектора или текста на странице.",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "url": { "type": "string" },
    "selector": { "type": "string" },
    "text": { "type": "string" },
    "timeout_ms": { "type": "integer", "minimum": 0 }
  }
}`),
		},
	}
}

func toolBrowserClose() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "browser.close",
			Description: "Закрыть текущую страницу (браузер остаётся запущенным для повторного использования).",
			Parameters: mustSchema(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {}
}`),
		},
	}
}
```

- [ ] **Step 11: Fix all callers of the updated functions**

Search for all callers and update them. Run:

```bash
go build ./...
```

The compiler will report every broken call site. Fix each one by adding `false` as the third argument (allowBrowser=false by default). Typical callers are in `internal/core/`, `internal/agent/`, and `internal/cli/`.

Example fix pattern:
```go
// Before:
tools := ListTools(allowExec, allowWeb)
// After:
tools := ListTools(allowExec, allowWeb, false) // allowBrowser added in CLI flag task
```

Similarly for ListToolsForMode — the new `allowBrowser` goes in the 4th position:
```go
// Before:
ListToolsForMode(mode, allowExec, allowWeb, hasSubtasks, hasQuestionAsker)
// After:
ListToolsForMode(mode, allowExec, allowWeb, false, hasSubtasks, hasQuestionAsker)
```

- [ ] **Step 12: Build clean**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 13: Run tests**

```bash
go test ./...
```

Expected: all tests pass.

- [ ] **Step 14: Commit**

```bash
git add internal/tools/registry.go
git commit -m "feat(browser): add browser tools to registry (allowBrowser param)"
```

---

## Task 8: call.go — add 10 dispatch cases

**Files:**
- Modify: `internal/tools/call.go`

- [ ] **Step 1: Add 10 cases to the `switch name` in `Call()`**

Add before the `default:` case:

```go
case "browser.navigate":
    var req BrowserNavigateRequest
    if err := decodeToolInput(input, &req); err != nil {
        return nil, err
    }
    resp, err := r.BrowserNavigate(ctx, req)
    if err != nil {
        return nil, err
    }
    return mustJSON(resp)

case "browser.snapshot":
    var req BrowserSnapshotRequest
    if err := decodeToolInput(input, &req); err != nil {
        return nil, err
    }
    resp, err := r.BrowserSnapshot(ctx, req)
    if err != nil {
        return nil, err
    }
    return mustJSON(resp)

case "browser.screenshot":
    var req BrowserScreenshotRequest
    if err := decodeToolInput(input, &req); err != nil {
        return nil, err
    }
    resp, err := r.BrowserScreenshot(ctx, req)
    if err != nil {
        return nil, err
    }
    return mustJSON(resp)

case "browser.click":
    var req BrowserClickRequest
    if err := decodeToolInput(input, &req); err != nil {
        return nil, err
    }
    resp, err := r.BrowserClick(ctx, req)
    if err != nil {
        return nil, err
    }
    return mustJSON(resp)

case "browser.type":
    var req BrowserTypeRequest
    if err := decodeToolInput(input, &req); err != nil {
        return nil, err
    }
    resp, err := r.BrowserType(ctx, req)
    if err != nil {
        return nil, err
    }
    return mustJSON(resp)

case "browser.fill":
    var req BrowserFillRequest
    if err := decodeToolInput(input, &req); err != nil {
        return nil, err
    }
    resp, err := r.BrowserFill(ctx, req)
    if err != nil {
        return nil, err
    }
    return mustJSON(resp)

case "browser.select":
    var req BrowserSelectRequest
    if err := decodeToolInput(input, &req); err != nil {
        return nil, err
    }
    resp, err := r.BrowserSelect(ctx, req)
    if err != nil {
        return nil, err
    }
    return mustJSON(resp)

case "browser.eval":
    var req BrowserEvalRequest
    if err := decodeToolInput(input, &req); err != nil {
        return nil, err
    }
    resp, err := r.BrowserEval(ctx, req)
    if err != nil {
        return nil, err
    }
    return mustJSON(resp)

case "browser.wait":
    var req BrowserWaitRequest
    if err := decodeToolInput(input, &req); err != nil {
        return nil, err
    }
    resp, err := r.BrowserWait(ctx, req)
    if err != nil {
        return nil, err
    }
    return mustJSON(resp)

case "browser.close":
    var req BrowserCloseRequest
    if err := decodeToolInput(input, &req); err != nil {
        return nil, err
    }
    resp, err := r.BrowserClose(ctx, req)
    if err != nil {
        return nil, err
    }
    return mustJSON(resp)
```

- [ ] **Step 2: Build and test**

```bash
go build ./...
go test ./internal/tools/... -v -run TestBrowser
```

Expected: builds clean, browser unit tests pass.

- [ ] **Step 3: Commit**

```bash
git add internal/tools/call.go
git commit -m "feat(browser): add browser tool dispatch in call.go"
```

---

## Task 9: CLI flag — --allow-browser

**Files:**
- Modify: `internal/cli/apply.go`

- [ ] **Step 1: Add `allowBrowser` variable and flag**

After `allowWeb bool` variable (around line 40), add:

```go
allowBrowser bool
```

In `init()` after the `--allow-web` flag registration:

```go
applyCmd.Flags().BoolVar(&allowBrowser, "allow-browser", false, "Allow browser.* tools (requires Node.js and npx)")
```

- [ ] **Step 2: Pass allowBrowser through the apply chain**

Find where `allowWebEffective` is passed to the agent/runner and add the parallel `allowBrowserEffective`:

```go
allowBrowserEffective := allowBrowser
// (no config override for browser — always requires explicit flag)
```

Where `RunnerOptions` is constructed (around line 453), add:

```go
Browser:     cfg.Browser,
AllowBrowser: allowBrowserEffective,
```

Where tools are listed for the agent (look for `ListTools` or `ListToolsForMode` calls), pass `allowBrowserEffective` as the third argument.

- [ ] **Step 3: Build**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 4: Verify the flag is registered**

```bash
go run ./cmd/orchestra apply --help
```

Expected: `--allow-browser` appears in the output.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/apply.go
git commit -m "feat(browser): add --allow-browser CLI flag to apply command"
```

---

## Task 10: ToolsVersion bump + test coverage

**Files:**
- Modify: `internal/protocol/version.go`
- Modify: `internal/tools/aliases_test.go`

- [ ] **Step 1: Bump ToolsVersion 7 → 8**

In `internal/protocol/version.go`, change:

```go
// v7: added fs.delete, fs.rename; git.status, git.log, git.diff (read, always);
//     git.commit, git.branch, git.checkout, git.push (write, allowExec-gated).
ToolsVersion = 7
```

To:

```go
// v7: added fs.delete, fs.rename; git.status, git.log, git.diff (read, always);
//     git.commit, git.branch, git.checkout, git.push (write, allowExec-gated).
// v8: added browser.navigate, browser.snapshot, browser.screenshot, browser.click,
//     browser.type, browser.fill, browser.select, browser.eval, browser.wait,
//     browser.close (all allowBrowser-gated).
ToolsVersion = 8
```

- [ ] **Step 2: Add browser gating test to `aliases_test.go`**

Add after `TestListTools_NewToolsPresent`:

```go
// TestListTools_BrowserGating verifies browser tools are absent without allowBrowser.
func TestListTools_BrowserGating(t *testing.T) {
    browserTools := []string{
        "browser.navigate", "browser.snapshot", "browser.screenshot",
        "browser.click", "browser.type", "browser.fill",
        "browser.select", "browser.eval", "browser.wait", "browser.close",
    }

    // Without allowBrowser: browser tools must not appear.
    without := ListTools(false, false, false)
    withoutNames := toolNameSet(without)
    for _, name := range browserTools {
        if withoutNames[name] {
            t.Errorf("ListTools(allowBrowser=false): must not include %q", name)
        }
    }

    // With allowBrowser: all browser tools must appear.
    with := ListTools(false, false, true)
    withNames := toolNameSet(with)
    for _, name := range browserTools {
        if !withNames[name] {
            t.Errorf("ListTools(allowBrowser=true): missing tool %q", name)
        }
    }
}
```

Also update `TestListTools_AllNamesPresent` to pass the new third argument:

```go
defs := ListTools(true, false, false)
```

And update `TestListTools_ExecGating` and `TestListTools_WebGating` similarly (add `, false` as third arg).

And update `TestListToolsForMode_NewModes`:

```go
general := ListToolsForMode("general", false, false, false, false, false)
```

- [ ] **Step 3: Run all tests**

```bash
go test ./...
```

Expected: all tests pass.

- [ ] **Step 4: Final build check**

```bash
go build ./...
go vet ./...
```

Expected: no errors or warnings.

- [ ] **Step 5: Commit**

```bash
git add internal/protocol/version.go internal/tools/aliases_test.go
git commit -m "feat(browser): bump ToolsVersion 7→8, add browser gating tests"
```

---

## Self-Review Checklist

After writing this plan, here is a check against the spec:

1. **BrowserConfig** — Task 1 ✅ (struct + field + defaults + validAgentToolNames)
2. **browser.Client** — Task 2 ✅ (New, ensureStarted, handshake, Call, doCall, Close)
3. **Client tests** — Task 3 ✅ (navigate, snapshot, multiple calls, timeout, missing binary, close idempotent)
4. **runner.go integration** — Task 4 ✅ (browserClient field, allowBrowserEval, NewRunner, Close)
5. **10 tool implementations** — Task 5 ✅ (all 10 with validation)
6. **Tool tests** — Task 6 ✅ (unit + integration gated by ORCH_E2E_BROWSER=1)
7. **registry.go ALL 7 locations** — Task 7 ✅ (ListTools, ListToolsWithMCP, ListToolsWithSubtasksAndMCP, ListToolsWithSubtasks, ListToolsForMode, listToolsBuild, listToolsGeneral + allToolDefsMap + applyParallelFlags)
8. **call.go** — Task 8 ✅ (10 dispatch cases)
9. **CLI --allow-browser** — Task 9 ✅
10. **ToolsVersion 7→8** — Task 10 ✅
11. **browser.eval double-gate** — Task 5 ✅ (checks allowBrowserEval in BrowserEval)
12. **npx not found error** — Task 2 ✅ (returns ExecFailed with install hint)
13. **Subprocess restart on crash** — Task 2 ✅ (doCall retries once if isSubprocessDead)
14. **Timeout** — Task 2 ✅ (ExecTimeout with select+time.After)
