// Package mcp implements a client for the Model Context Protocol (MCP).
// MCP servers expose tools over JSON-RPC 2.0 via stdio subprocess.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

const mcpProtocolVersion = "2024-11-05"

// MCPTool is a tool definition returned by an MCP server.
type MCPTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// MCPContentItem is one piece of content in a tool response.
type MCPContentItem struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int64 `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Client manages a single MCP server subprocess.
type Client struct {
	name    string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Scanner
	tools   []MCPTool
	idSeq   atomic.Int64
	mu      sync.Mutex
	pending map[int64]chan rpcResponse
	done    chan struct{}
}

// Start spawns the MCP server and performs the initialize handshake.
func Start(ctx context.Context, name string, command []string, env map[string]string) (*Client, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("mcp %q: command is empty", name)
	}

	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Env = buildEnv(env)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp %q: stdin pipe: %w", name, err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp %q: stdout pipe: %w", name, err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp %q: start: %w", name, err)
	}

	c := &Client{
		name:    name,
		cmd:     cmd,
		stdin:   stdin,
		stdout:  bufio.NewScanner(stdoutPipe),
		pending: make(map[int64]chan rpcResponse),
		done:    make(chan struct{}),
	}
	// Set a generous scan buffer for large tool descriptions.
	c.stdout.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)

	go c.readLoop()

	// Initialize handshake.
	if err := c.initialize(ctx); err != nil {
		c.Close()
		return nil, fmt.Errorf("mcp %q: initialize: %w", name, err)
	}

	// Discover tools.
	tools, err := c.listTools(ctx)
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("mcp %q: tools/list: %w", name, err)
	}
	c.tools = tools

	return c, nil
}

// Tools returns the tools advertised by this server.
func (c *Client) Tools() []MCPTool { return c.tools }

// ServerName returns the configured name of this server.
func (c *Client) ServerName() string { return c.name }

// Call invokes a tool on the MCP server and returns the combined text output.
func (c *Client) Call(ctx context.Context, toolName string, arguments json.RawMessage) (string, error) {
	params := map[string]any{
		"name":      toolName,
		"arguments": json.RawMessage(arguments),
	}
	raw, err := c.call(ctx, "tools/call", params)
	if err != nil {
		return "", err
	}

	var result struct {
		Content []MCPContentItem `json:"content"`
		IsError bool             `json:"isError"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return string(raw), nil // return raw if parse fails
	}

	var out string
	for _, item := range result.Content {
		if item.Type == "text" {
			out += item.Text
		}
	}
	if result.IsError {
		return "", fmt.Errorf("mcp tool error: %s", out)
	}
	return out, nil
}

// Close stops the MCP server subprocess.
func (c *Client) Close() error {
	_ = c.stdin.Close()
	select {
	case <-c.done:
	case <-time.After(5 * time.Second):
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		<-c.done
	}
	return c.cmd.Wait()
}

func (c *Client) initialize(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "orchestra", "version": "vnext"},
	}
	if _, err := c.call(ctx, "initialize", params); err != nil {
		return err
	}
	// Send initialized notification (no ID = notification, no response expected).
	return c.notify("notifications/initialized", nil)
}

func (c *Client) listTools(ctx context.Context) ([]MCPTool, error) {
	raw, err := c.call(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Tools []MCPTool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("parse tools/list: %w", err)
	}
	return result.Tools, nil
}

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.idSeq.Add(1)
	ch := make(chan rpcResponse, 1)

	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	req := rpcRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: params}
	if err := c.send(req); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("mcp rpc error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	case <-c.done:
		return nil, fmt.Errorf("mcp server %q exited", c.name)
	}
}

func (c *Client) notify(method string, params any) error {
	return c.send(rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
}

func (c *Client) send(req rpcRequest) error {
	b, err := json.Marshal(req)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	c.mu.Lock()
	_, err = c.stdin.Write(b)
	c.mu.Unlock()
	return err
}

func (c *Client) readLoop() {
	defer close(c.done)
	for c.stdout.Scan() {
		line := c.stdout.Bytes()
		var resp rpcResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue // skip malformed lines (e.g. server startup logs)
		}
		if resp.ID == nil {
			continue // notification — ignore
		}
		c.mu.Lock()
		ch, ok := c.pending[*resp.ID]
		if ok {
			delete(c.pending, *resp.ID)
		}
		c.mu.Unlock()
		if ok {
			ch <- resp
		}
	}
}

func buildEnv(extra map[string]string) []string {
	env := os.Environ()
	for k, v := range extra {
		env = append(env, k+"="+os.ExpandEnv(v))
	}
	return env
}
