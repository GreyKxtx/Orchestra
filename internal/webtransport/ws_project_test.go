package webtransport

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/orchestra/orchestra/internal/core"
	"github.com/orchestra/orchestra/internal/projects"
	"github.com/orchestra/orchestra/protocol/jsonrpc"
)

func startProjectServer(t *testing.T) (base string, reg *projects.Registry) {
	t.Helper()
	_ = t.TempDir() // register the temp-dir RemoveAll before reg.Shutdown; see startRegistryServer
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	reg = projects.NewRegistry(core.Options{})
	t.Cleanup(reg.Shutdown)

	base, stop, err := Serve(ctx, Options{
		Token:    "secret",
		Health:   map[string]any{"status": "ok"},
		Registry: reg,
		NewProjectHandler: func(c *core.Core) (jsonrpc.Handler, func(*jsonrpc.Server)) {
			h := core.NewRPCHandler(c)
			return h, func(srv *jsonrpc.Server) {
				h.SetNotifier(srv)
				h.SetRequester(srv.Request)
			}
		},
		NewHandler: func() (jsonrpc.Handler, func(*jsonrpc.Server)) { return nil, nil },
	})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	t.Cleanup(func() { _ = stop() })
	return base, reg
}

func dialProject(t *testing.T, base, id string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return websocket.Dial(ctx, "ws"+base[len("http"):]+"/ws?project="+id, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer secret"}},
	})
}

// Two projects, two sockets, each core.health reporting its own workspace. This
// is the test that proves the projects do not bleed into each other over the
// wire, not just in the registry.
func TestWS_TwoProjectsServeTheirOwnCores(t *testing.T) {
	base, reg := startProjectServer(t)
	rootA := initWS(t)
	rootB := initWS(t)

	pa, err := reg.Open(context.Background(), rootA)
	if err != nil {
		t.Fatalf("open a: %v", err)
	}
	pb, err := reg.Open(context.Background(), rootB)
	if err != nil {
		t.Fatalf("open b: %v", err)
	}

	health := func(id string) map[string]any {
		t.Helper()
		c, _, err := dialProject(t, base, id)
		if err != nil {
			t.Fatalf("dial %s: %v", id, err)
		}
		t.Cleanup(func() { _ = c.CloseNow() })
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "core.health"})
		if err := c.Write(context.Background(), websocket.MessageText, b); err != nil {
			t.Fatalf("write: %v", err)
		}
		rctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, raw, err := c.Read(rctx)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		res, _ := m["result"].(map[string]any)
		return res
	}

	ha := health(pa.ID)
	hb := health(pb.ID)
	if ha["workspace_root"] == hb["workspace_root"] {
		t.Fatalf("both sockets served the same workspace %v — projects are bleeding", ha["workspace_root"])
	}
	if ha["workspace_root"] != rootA {
		t.Fatalf("project A served %v, want %q", ha["workspace_root"], rootA)
	}
	if hb["workspace_root"] != rootB {
		t.Fatalf("project B served %v, want %q", hb["workspace_root"], rootB)
	}
}

// The single-connection rule is per project, not global — otherwise opening a
// second project would be refused because the first one has a tab open.
func TestWS_ConnectionGuardIsPerProject(t *testing.T) {
	base, reg := startProjectServer(t)
	pa, err := reg.Open(context.Background(), initWS(t))
	if err != nil {
		t.Fatalf("open a: %v", err)
	}
	pb, err := reg.Open(context.Background(), initWS(t))
	if err != nil {
		t.Fatalf("open b: %v", err)
	}

	ca, _, err := dialProject(t, base, pa.ID)
	if err != nil {
		t.Fatalf("first dial a: %v", err)
	}
	t.Cleanup(func() { _ = ca.CloseNow() })

	// B is unaffected by A's live connection.
	cb, _, err := dialProject(t, base, pb.ID)
	if err != nil {
		t.Fatalf("dial b while a is live: %v — the guard is global, not per project", err)
	}
	t.Cleanup(func() { _ = cb.CloseNow() })

	// A second connection to A is still refused.
	_, resp, err := dialProject(t, base, pa.ID)
	if err == nil {
		t.Fatal("second connection to project A was accepted")
	}
	if resp == nil || resp.StatusCode != http.StatusConflict {
		t.Fatalf("second connection to A → %v, want 409", resp)
	}
}

func TestWS_UnknownProjectIs404(t *testing.T) {
	base, _ := startProjectServer(t)
	_, resp, err := dialProject(t, base, "sha256:nope")
	if err == nil {
		t.Fatal("a socket for an unopened project was accepted")
	}
	if resp == nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown project → %v, want 404", resp)
	}
}
