package e2e_real_llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orchestra/orchestra/protocol"
)

type rpcEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data,omitempty"`
	} `json:"error,omitempty"`
}

type coreRPCClient struct {
	t       *testing.T
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	reader  *bufio.Reader
	nextID  int
	mu      sync.Mutex
	events  []rpcEnvelope
	cancel  context.CancelFunc
}

func startCoreRPC(t *testing.T, projectRoot string) *coreRPCClient {
	t.Helper()
	binary := findOrchestraBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)

	cmd := exec.CommandContext(ctx, binary, "core", "--workspace-root", projectRoot)
	cmd.Env = append(os.Environ(), "ORCH_DEBUG=0")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start core: %v", err)
	}

	c := &coreRPCClient{
		t:      t,
		cmd:    cmd,
		stdin:  stdin,
		reader: bufio.NewReader(stdout),
		nextID: 1,
		cancel: cancel,
	}
	t.Cleanup(c.close)
	return c
}

func (c *coreRPCClient) close() {
	c.cancel()
	_ = c.stdin.Close()
	done := make(chan error, 1)
	go func() { done <- c.cmd.Wait() }()
	select {
	case <-time.After(2 * time.Second):
		_ = c.cmd.Process.Kill()
	case <-done:
	}
}

func (c *coreRPCClient) writeFrame(payload []byte) {
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(payload))
	if _, err := io.WriteString(c.stdin, header); err != nil {
		c.t.Fatal(err)
	}
	if _, err := c.stdin.Write(payload); err != nil {
		c.t.Fatal(err)
	}
}

func (c *coreRPCClient) readFrame() rpcEnvelope {
	for {
		var contentLen int
		for {
			line, err := c.reader.ReadString('\n')
			if err != nil {
				c.t.Fatalf("read header: %v", err)
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				break
			}
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(parts[0]), "content-length") {
				contentLen, err = strconv.Atoi(strings.TrimSpace(parts[1]))
				if err != nil {
					c.t.Fatalf("bad Content-Length: %q", parts[1])
				}
			}
		}
		if contentLen <= 0 {
			c.t.Fatal("missing Content-Length")
		}
		payload := make([]byte, contentLen)
		if _, err := io.ReadFull(c.reader, payload); err != nil {
			c.t.Fatalf("read payload: %v", err)
		}
		var env rpcEnvelope
		if err := json.Unmarshal(payload, &env); err != nil {
			c.t.Fatalf("unmarshal: %v payload=%s", err, string(payload))
		}
		if len(env.ID) == 0 && env.Method != "" {
			c.mu.Lock()
			c.events = append(c.events, env)
			c.mu.Unlock()
			continue
		}
		return env
	}
}

func (c *coreRPCClient) call(method string, params any) json.RawMessage {
	c.t.Helper()
	id := c.nextID
	c.nextID++
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		c.t.Fatal(err)
	}
	c.writeFrame(body)
	for {
		resp := c.readFrame()
		var gotID int
		if err := json.Unmarshal(resp.ID, &gotID); err != nil || gotID != id {
			c.t.Fatalf("unexpected response id=%s want=%d method=%s", string(resp.ID), id, method)
		}
		if resp.Error != nil {
			c.t.Fatalf("rpc %s error: %s", method, resp.Error.Message)
		}
		return resp.Result
	}
}

func (c *coreRPCClient) drainAgentEvents() []rpcEnvelope {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := append([]rpcEnvelope(nil), c.events...)
	c.events = c.events[:0]
	return out
}

func (c *coreRPCClient) initialize(projectRoot string) {
	healthRaw := c.call("core.health", map[string]any{})
	var health map[string]any
	if err := json.Unmarshal(healthRaw, &health); err != nil {
		c.t.Fatal(err)
	}
	projectID, _ := health["project_id"].(string)
	if projectID == "" {
		projectID = "e2e:test"
	}
	c.call("initialize", map[string]any{
		"project_root":     projectRoot,
		"project_id":       projectID,
		"protocol_version": protocol.ProtocolVersion,
		"tools_version":    protocol.ToolsVersion,
		"ops_version":      protocol.OpsVersion,
	})
}

func (c *coreRPCClient) sessionStart(sessionID string) string {
	params := map[string]any{}
	if sessionID != "" {
		params["session_id"] = sessionID
	}
	raw := c.call("session.start", params)
	var res struct {
		SessionID string `json:"session_id"`
		Restored  bool   `json:"restored"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		c.t.Fatal(err)
	}
	if res.SessionID == "" {
		c.t.Fatal("empty session_id")
	}
	return res.SessionID
}

func (c *coreRPCClient) sessionMessage(sessionID, content string) {
	c.call("session.message", map[string]any{
		"session_id": sessionID,
		"content":    content,
		"apply":      false,
		"profile":    "fast",
	})
}

func (c *coreRPCClient) sessionMessageWithAttachments(sessionID, content string, attachments []map[string]any) error {
	c.t.Helper()
	id := c.nextID
	c.nextID++
	params := map[string]any{
		"session_id": sessionID,
		"content":    content,
		"apply":      false,
		"profile":    "fast",
	}
	if len(attachments) > 0 {
		params["attachments"] = attachments
	}
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "session.message",
		"params":  params,
	})
	if err != nil {
		return err
	}
	c.writeFrame(body)
	for {
		resp := c.readFrame()
		var gotID int
		if err := json.Unmarshal(resp.ID, &gotID); err != nil || gotID != id {
			c.t.Fatalf("unexpected response id=%s want=%d method=session.message", string(resp.ID), id)
		}
		if resp.Error != nil {
			return fmt.Errorf("%s", resp.Error.Message)
		}
		return nil
	}
}

func (c *coreRPCClient) sessionHistoryLen(sessionID string) int {
	raw := c.call("session.history", map[string]any{"session_id": sessionID})
	var res struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		c.t.Fatal(err)
	}
	return len(res.Messages)
}

func agentEventsWithTurnID(events []rpcEnvelope, sessionID, turnID string) bool {
	for _, ev := range events {
		if ev.Method != "agent/event" {
			continue
		}
		var p struct {
			SessionID string `json:"session_id"`
			TurnID    string `json:"turn_id"`
		}
		if json.Unmarshal(ev.Params, &p) != nil {
			continue
		}
		if p.SessionID == sessionID && p.TurnID == turnID {
			return true
		}
	}
	return false
}

func firstAgentEventTurnID(events []rpcEnvelope) string {
	for _, ev := range events {
		if ev.Method != "agent/event" {
			continue
		}
		var p struct {
			TurnID string `json:"turn_id"`
		}
		if json.Unmarshal(ev.Params, &p) == nil && p.TurnID != "" {
			return p.TurnID
		}
	}
	return ""
}
