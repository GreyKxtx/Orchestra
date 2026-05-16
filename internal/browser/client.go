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
