package webtransport_test

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/core"
	"github.com/orchestra/orchestra/internal/webtransport"
	"github.com/orchestra/orchestra/patch/cache"
	"github.com/orchestra/orchestra/protocol"
	"github.com/orchestra/orchestra/protocol/jsonrpc"
)

// TestWS_RealCoreHandshakeAndSession drives an unmodified core.RPCHandler over
// the WebSocket transport. If this passes, the spec's central claim holds: the
// core needed no changes to gain a second transport.
func TestWS_RealCoreHandshakeAndSession(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig(root)
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	c, err := core.New(root, core.Options{})
	if err != nil {
		t.Fatalf("core.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	base, stop, err := webtransport.Serve(ctx, webtransport.Options{
		Token:  "tok",
		Health: c.Health(),
		NewHandler: func() (jsonrpc.Handler, func(*jsonrpc.Server)) {
			h := core.NewRPCHandler(c)
			return h, func(srv *jsonrpc.Server) {
				h.SetNotifier(srv)
				h.SetRequester(srv.Request)
			}
		},
	})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	t.Cleanup(func() { _ = stop() })

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dialCancel()
	conn, _, err := websocket.Dial(dialCtx, "ws"+base[len("http"):]+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer tok"}},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })

	call := func(id int, method string, params any) map[string]any {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
		if err := conn.Write(context.Background(), websocket.MessageText, b); err != nil {
			t.Fatalf("write %s: %v", method, err)
		}
		readCtx, cancelRead := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancelRead()
		for {
			_, raw, err := conn.Read(readCtx)
			if err != nil {
				t.Fatalf("read after %s: %v", method, err)
			}
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatalf("unmarshal %q: %v", raw, err)
			}
			// Skip notifications the core emits along the way.
			if m["id"] == nil {
				continue
			}
			if e, ok := m["error"]; ok && e != nil {
				t.Fatalf("%s returned error: %v", method, e)
			}
			return m
		}
	}

	projectID, err := cache.ComputeProjectID(root)
	if err != nil {
		t.Fatalf("ComputeProjectID: %v", err)
	}
	call(1, "initialize", core.InitializeParams{
		ProjectRoot:     root,
		ProjectID:       projectID,
		ProtocolVersion: protocol.ProtocolVersion,
		OpsVersion:      protocol.OpsVersion,
		ToolsVersion:    protocol.ToolsVersion,
	})

	startResp := call(2, "session.start", map[string]any{})
	startRes, _ := startResp["result"].(map[string]any)
	sid, _ := startRes["session_id"].(string)
	if sid == "" {
		t.Fatalf("session.start returned no session_id: %v", startRes)
	}

	// session.list reads the sessions the core has written to disk
	// (internal/core/session_rpc.go:186), and a session that has only been
	// started has nothing to write yet. Persist one message through the
	// documented write path first, so the list assertion below is about the
	// round trip rather than about a promise the core never made.
	call(3, "session.ui_sync", map[string]any{
		"session_id": sid,
		"title":      "over the socket",
		"ui_messages": []map[string]any{
			{"role": "user", "text": "hello from the browser"},
		},
	})

	listResp := call(4, "session.list", map[string]any{})
	listRes, _ := listResp["result"].(map[string]any)
	sessions, _ := listRes["sessions"].([]any)
	found := false
	for _, s := range sessions {
		if m, ok := s.(map[string]any); ok && m["id"] == sid {
			found = true
		}
	}
	if !found {
		t.Fatalf("session %q missing from session.list: %v", sid, listRes)
	}
}
