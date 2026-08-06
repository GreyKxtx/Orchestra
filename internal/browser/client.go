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
	// EnvOverride sets cmd.Env on the subprocess — for tests only.
	// When non-empty, replaces the inherited environment entirely.
	// Pass append(os.Environ(), "KEY=val") to extend the parent env.
	EnvOverride []string
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
	var cmd *exec.Cmd
	if len(c.cfg.CmdOverride) > 0 {
		if len(c.cfg.CmdOverride) == 1 {
			cmd = exec.Command(c.cfg.CmdOverride[0])
		} else {
			cmd = exec.Command(c.cfg.CmdOverride[0], c.cfg.CmdOverride[1:]...)
		}
	} else {
		args := []string{"--yes", "@playwright/mcp@latest"}
		if c.cfg.Headless {
			args = append(args, "--headless")
		}
		args = append(args, "--viewport-size",
			fmt.Sprintf("%d,%d", c.cfg.ViewportWidth, c.cfg.ViewportHeight))
		cmd = exec.Command("npx", args...)
	}
	if len(c.cfg.EnvOverride) > 0 {
		cmd.Env = c.cfg.EnvOverride
	}
	return cmd
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
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	type res struct {
		err error
	}
	ch := make(chan res, 1) 
	go func() {
		ch <- res{err: c.handshakeIO()}
	}()

	select {
	case <-ctx.Done():
		c.abortInFlight(func() { <-ch })
		return protocol.NewError(protocol.ExecTimeout, "browser initialize timed out", nil)
	case <-timer.C:
		c.abortInFlight(func() { <-ch })
		return protocol.NewError(protocol.ExecTimeout, "browser initialize timed out", nil)
	case r := <-ch:
		if r.err != nil {
			return protocol.NewError(protocol.ExecFailed,
				fmt.Sprintf("browser initialize: %v", r.err), nil)
		}
		return nil
	}
}

func (c *Client) handshakeIO() error {
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
	if c.enc == nil || c.dec == nil {
		return fmt.Errorf("encode initialize: subprocess not started")
	}
	if err := c.enc.Encode(req); err != nil {
		return fmt.Errorf("encode initialize: %w", err)
	}
	var resp mcpResponse
	if err := c.dec.Decode(&resp); err != nil {
		return fmt.Errorf("decode initialize response: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("initialize error: %s", resp.Error.Message)
	}
	_ = c.enc.Encode(mcpRequest{JSONRPC: "2.0", Method: "notifications/initialized"})
	return nil
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
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	type res struct {
		result *MCPResult
		err    error
	}
	ch := make(chan res, 1)
	go func() {
		result, err := c.callIO(req)
		ch <- res{result: result, err: err}
	}()

	select {
	case <-ctx.Done():
		c.abortInFlight(func() { <-ch })
		c.ready = false
		return nil, protocol.NewError(protocol.ExecTimeout, "browser operation timed out", nil)
	case <-timer.C:
		c.abortInFlight(func() { <-ch })
		c.ready = false
		return nil, protocol.NewError(protocol.ExecTimeout, "browser operation timed out", nil)
	case r := <-ch:
		return r.result, r.err
	}
}

func (c *Client) callIO(req mcpRequest) (*MCPResult, error) {
	if c.enc == nil || c.dec == nil {
		return nil, protocol.NewError(protocol.ExecFailed, "encode request: subprocess not started", nil)
	}
	if err := c.enc.Encode(req); err != nil {
		return nil, protocol.NewError(protocol.ExecFailed,
			fmt.Sprintf("encode request: %v", err), nil)
	}
	var resp mcpResponse
	if err := c.dec.Decode(&resp); err != nil {
		return nil, protocol.NewError(protocol.ExecFailed,
			fmt.Sprintf("decode response: %v", err), nil)
	}
	if resp.Error != nil {
		return nil, protocol.NewError(protocol.ExecFailed, resp.Error.Message, nil)
	}
	var result MCPResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, protocol.NewError(protocol.ExecFailed,
			fmt.Sprintf("parse MCP result: %v", err), nil)
	}
	if result.IsError {
		msg := result.TextContent()
		if msg == "" {
			msg = "browser tool error"
		}
		return nil, protocol.NewError(protocol.ExecFailed, msg, nil)
	}
	return &result, nil
}

// abortInFlight stops a hung MCP I/O goroutine without racing the json
// codec: kill the child so pipe reads get EOF, wait for the goroutine to
// finish (via wait), then clear encoder/decoder pointers.
func (c *Client) abortInFlight(wait func()) {
	c.killProcessOnly()
	if wait != nil {
		wait()
	}
	c.clearSubprocess()
}

func (c *Client) isSubprocessDead() bool {
	if c.cmd == nil || c.cmd.Process == nil {
		return true
	}
	return c.cmd.ProcessState != nil
}

// killProcessOnly signals the child to exit so blocked pipe I/O unblocks
// with EOF. Does not Close parent pipe ends or nil enc/dec — callers must
// wait for in-flight Encode/Decode first (see abortInFlight).
func (c *Client) killProcessOnly() {
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	}
}

func (c *Client) clearSubprocess() {
	if c.stdin != nil {
		_ = c.stdin.Close()
		c.stdin = nil
	}
	if c.stdout != nil {
		_ = c.stdout.Close()
		c.stdout = nil
	}
	c.cmd = nil
	c.enc = nil
	c.dec = nil
}

func (c *Client) killSubprocess() {
	c.killProcessOnly()
	c.clearSubprocess()
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
