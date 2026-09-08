package webtransport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/protocol/jsonrpc"
)

// The /ws transport is part of the stable contract, so it carries a version.
func TestProtocolVersionIsAtLeast15(t *testing.T) {
	if protocol.ProtocolVersion < 15 {
		t.Fatalf("ProtocolVersion = %d, want >= 15 — /ws is a supported transport",
			protocol.ProtocolVersion)
	}
}

// askHandler is a handler whose only job is to exercise the transport: "echo"
// answers immediately, "ask" issues a server-initiated request and reports what
// came back. The core's own behaviour is covered by internal/core's tests; what
// is under test here is that the bidirectional wiring survives the socket.
type askHandler struct {
	srv *jsonrpc.Server

	mu      sync.Mutex
	askErr  error
	askDone chan struct{}
	askOnce sync.Once
}

func newAskHandler() *askHandler { return &askHandler{askDone: make(chan struct{})} }

func (h *askHandler) attach(srv *jsonrpc.Server) { h.srv = srv }

func (h *askHandler) Handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case "echo":
		return map[string]any{"ok": true}, nil
	case "notify":
		return nil, h.srv.Notify("agent/event", map[string]any{"type": "message_delta", "content": "hi"})
	case "ask":
		var out struct {
			Approved bool `json:"approved"`
		}
		err := h.srv.Request(ctx, "permission/request", map[string]any{"tool": "bash"}, &out)
		h.mu.Lock()
		h.askErr = err
		h.mu.Unlock()
		h.askOnce.Do(func() { close(h.askDone) })
		if err != nil {
			return nil, err
		}
		return map[string]any{"approved": out.Approved}, nil
	}
	return nil, errors.New("unknown method")
}

func (h *askHandler) result() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.askErr
}

func startTestServer(t *testing.T) (base string, h *askHandler) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h = newAskHandler()
	base, stop, err := Serve(ctx, Options{
		Token:  "secret",
		Health: map[string]any{"status": "ok"},
		NewHandler: func() (jsonrpc.Handler, func(*jsonrpc.Server)) {
			return h, h.attach
		},
	})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	t.Cleanup(func() { _ = stop() })
	return base, h
}

func dial(t *testing.T, base, token string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	opts := &websocket.DialOptions{HTTPHeader: http.Header{}}
	if token != "" {
		opts.HTTPHeader.Set("Authorization", "Bearer "+token)
	}
	c, _, err := websocket.Dial(ctx, "ws"+base[len("http"):]+"/ws", opts)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	return c
}

func send(t *testing.T, c *websocket.Conn, v any) {
	t.Helper()
	b, _ := json.Marshal(v)
	if err := c.Write(context.Background(), websocket.MessageText, b); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func recv(t *testing.T, c *websocket.Conn) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, b, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal %q: %v", b, err)
	}
	return m
}

func TestServe_RequestResponseRoundTrip(t *testing.T) {
	base, _ := startTestServer(t)
	c := dial(t, base, "secret")

	send(t, c, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "echo"})
	got := recv(t, c)
	if got["id"] != float64(1) {
		t.Fatalf("id = %v, want 1", got["id"])
	}
	res, _ := got["result"].(map[string]any)
	if res["ok"] != true {
		t.Fatalf("result = %v, want {ok:true}", got["result"])
	}
}

func TestServe_ServerNotificationReachesTheClient(t *testing.T) {
	base, _ := startTestServer(t)
	c := dial(t, base, "secret")

	send(t, c, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "notify"})
	// The notification and the response may arrive in either order.
	sawEvent := false
	for i := 0; i < 2; i++ {
		m := recv(t, c)
		if m["method"] == "agent/event" {
			sawEvent = true
		}
	}
	if !sawEvent {
		t.Fatal("no agent/event notification arrived over the socket")
	}
}

func TestServe_ServerInitiatedRequestIsAnswered(t *testing.T) {
	base, h := startTestServer(t)
	c := dial(t, base, "secret")

	send(t, c, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ask"})

	ask := recv(t, c)
	if ask["method"] != "permission/request" {
		t.Fatalf("first message = %v, want permission/request", ask["method"])
	}
	send(t, c, map[string]any{"jsonrpc": "2.0", "id": ask["id"], "result": map[string]any{"approved": true}})

	resp := recv(t, c)
	res, _ := resp["result"].(map[string]any)
	if res["approved"] != true {
		t.Fatalf("result = %v, want approved:true", resp["result"])
	}
	if err := h.result(); err != nil {
		t.Fatalf("handler saw error: %v", err)
	}
}

// This is the test that justifies WebSocket over SSE. A tab closed with a
// permission prompt on screen must not leave the tool waiting forever.
func TestServe_DisconnectMidPermissionFailsClosed(t *testing.T) {
	base, h := startTestServer(t)
	c := dial(t, base, "secret")

	send(t, c, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ask"})
	if m := recv(t, c); m["method"] != "permission/request" {
		t.Fatalf("expected permission/request, got %v", m["method"])
	}

	// The user closes the tab while the prompt is up.
	_ = c.CloseNow()

	select {
	case <-h.askDone:
	case <-time.After(10 * time.Second):
		t.Fatal("permission request never resolved after the client vanished — it hung")
	}
	if h.result() == nil {
		t.Fatal("permission request succeeded after the client vanished; it must fail closed")
	}
}

func TestServe_SecondConcurrentConnectionIsRejected(t *testing.T) {
	base, _ := startTestServer(t)
	first := dial(t, base, "secret")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, "ws"+base[len("http"):]+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer secret"}},
	})
	if err == nil {
		t.Fatal("second concurrent connection was accepted; MCP prompts would be stolen from the first")
	}
	if resp == nil || resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %v, want 409", resp)
	}

	// After the first goes away, a new client is accepted.
	_ = first.CloseNow()
	deadline := time.Now().Add(5 * time.Second)
	for {
		c2, _, err := websocket.Dial(ctx, "ws"+base[len("http"):]+"/ws", &websocket.DialOptions{
			HTTPHeader: http.Header{"Authorization": []string{"Bearer secret"}},
		})
		if err == nil {
			_ = c2.CloseNow()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("slot never freed after the first client closed: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestServe_HandshakeWithoutTokenIsRejected(t *testing.T) {
	base, _ := startTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, resp, err := websocket.Dial(ctx, "ws"+base[len("http"):]+"/ws", nil)
	if err == nil {
		t.Fatal("unauthenticated handshake was accepted")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %v, want 401", resp)
	}
}
