package rpcclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"github.com/orchestra/orchestra/internal/jsonrpc"
	"github.com/orchestra/orchestra/internal/protocol"
)

// Config configures the spawn + initialize handshake.
type Config struct {
	Binary        string // path to the orchestra executable
	WorkspaceRoot string // project root for `--workspace-root`
	ProjectID     string // optional, passed to initialize
}

// Client wraps a running `orchestra core` subprocess.
type Client struct {
	cfg Config

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	rpc *jsonrpc.Client

	events chan Event

	closeOnce sync.Once
	mu        sync.Mutex
	closed    bool
}

// Spawn starts the orchestra core subprocess and runs the initialize handshake.
// On any error during spawn or initialize, the subprocess is killed and the
// error is returned.
func Spawn(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.Binary == "" {
		return nil, fmt.Errorf("rpcclient: Config.Binary is empty")
	}
	if cfg.WorkspaceRoot == "" {
		return nil, fmt.Errorf("rpcclient: Config.WorkspaceRoot is empty")
	}

	cmd := exec.CommandContext(ctx, cfg.Binary, "core", "--workspace-root", cfg.WorkspaceRoot)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("rpcclient: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("rpcclient: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("rpcclient: stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("rpcclient: start: %w", err)
	}

	c := &Client{
		cfg:    cfg,
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
		rpc:    jsonrpc.NewClient(stdout, stdin),
		events: make(chan Event, 64),
	}

	// Drain stderr to avoid pipe blocking.
	go func() {
		buf := make([]byte, 4096)
		for {
			_, err := stderr.Read(buf)
			if err != nil {
				return
			}
		}
	}()

	c.rpc.SetNotificationHandler(c.handleNotification)

	c.send(Event{Kind: EventConnecting})
	initParams := map[string]any{
		"project_root":     cfg.WorkspaceRoot,
		"project_id":       cfg.ProjectID,
		"protocol_version": protocol.ProtocolVersion,
		"ops_version":      protocol.OpsVersion,
		"tools_version":    protocol.ToolsVersion,
	}
	var initResult map[string]any
	if err := c.rpc.Call(ctx, "initialize", initParams, &initResult); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("rpcclient: initialize: %w", err)
	}
	c.send(Event{Kind: EventInitialized})

	return c, nil
}

// Events returns the channel of streaming events.
// Closed when the connection terminates (subprocess exit or Close).
func (c *Client) Events() <-chan Event {
	return c.events
}

// AgentRun calls agent.run on the core. Streaming events arrive via Events().
// Returns when the agent.run RPC completes (final result returned).
func (c *Client) AgentRun(ctx context.Context, query string) error {
	params := map[string]any{
		"query": query,
		"apply": false, // Phase 2: dry-run only
	}
	var result map[string]any
	err := c.rpc.Call(ctx, "agent.run", params, &result)
	if err != nil {
		c.send(Event{Kind: EventError, Err: err.Error()})
	}
	c.send(Event{Kind: EventAgentRunCompleted})
	return err
}

// Close kills the subprocess and closes the events channel.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()

		_ = c.stdin.Close()
		if c.cmd != nil && c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		close(c.events)
	})
	return nil
}

func (c *Client) send(ev Event) {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return
	}
	// Non-blocking send: drop on backpressure (UI tolerates missed token deltas).
	select {
	case c.events <- ev:
	default:
	}
}

func (c *Client) handleNotification(method string, params json.RawMessage) {
	switch method {
	case "agent/event":
		c.handleAgentEvent(params)
	case "exec/output_chunk":
		c.handleExecOutput(params)
	}
}

func (c *Client) handleAgentEvent(params json.RawMessage) {
	var p struct {
		Step         int             `json:"step"`
		Type         string          `json:"type"`
		Content      string          `json:"content"`
		ToolCallID   string          `json:"tool_call_id"`
		ToolCallName string          `json:"tool_call_name"`
		Data         json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	ev := Event{
		Kind:         EventKind(p.Type),
		Step:         p.Step,
		Content:      p.Content,
		ToolCallID:   p.ToolCallID,
		ToolCallName: p.ToolCallName,
	}
	if EventKind(p.Type) == EventPendingOps && len(p.Data) > 0 {
		var payload PendingOpsPayload
		if err := json.Unmarshal(p.Data, &payload); err == nil {
			ev.PendingOps = &payload
		}
	}
	c.send(ev)
}

func (c *Client) handleExecOutput(params json.RawMessage) {
	var p struct {
		Step  int    `json:"step"`
		Chunk string `json:"chunk"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	c.send(Event{Kind: EventExecOutputChunk, Step: p.Step, Content: p.Chunk})
}
