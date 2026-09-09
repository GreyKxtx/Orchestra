package webtransport

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
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

// Closing a project through the API while a tab is connected must drop that
// socket first and only then close the core: the handler goroutines dereference
// the core's tools, and jsonrpc.Server does not recover panics. It must also
// free the guard slot, or re-opening the same path gets 409 on the next dial.
func TestWS_CloseProjectDropsItsLiveSocketAndFreesTheSlot(t *testing.T) {
	base, reg := startProjectServer(t)
	root := initWS(t)
	pa, err := reg.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	c, _, err := dialProject(t, base, pa.ID)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })

	if status, body := doJSON(t, http.MethodDelete, base+"/api/projects/"+pa.ID, nil); status != http.StatusNoContent {
		t.Fatalf("DELETE → %d %v, want 204", status, body)
	}

	rctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := c.Read(rctx); err == nil {
		t.Fatal("the socket of a closed project stayed open")
	} else if rctx.Err() != nil {
		t.Fatal("the socket of a closed project was not dropped within 5s")
	}

	status, body := doJSON(t, http.MethodPost, base+"/api/projects", map[string]any{"path": root})
	if status != http.StatusCreated {
		t.Fatalf("re-open → %d %v, want 201", status, body)
	}
	id, _ := body["id"].(string)
	c2, resp, err := dialProject(t, base, id)
	if err != nil {
		code := 0
		if resp != nil {
			code = resp.StatusCode
		}
		t.Fatalf("dial after re-open: %v (status %d) — the guard slot was not released", err, code)
	}
	_ = c2.CloseNow()
}

// A dial racing a DELETE of the same project must never end with a request
// served on a closed core (which would panic the process): either the dial is
// refused, or the connection is dropped before the core closes. There is no
// deterministic interleaving to assert, so this is a regression loop — a
// process crash or a -race report is the failure.
func TestWS_DialRacingDeleteNeverServesAClosedCore(t *testing.T) {
	base, reg := startProjectServer(t)
	root := initWS(t)
	for i := 0; i < 12; i++ {
		p, err := reg.Open(context.Background(), root)
		if err != nil {
			t.Fatalf("iteration %d open: %v", i, err)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			c, _, err := dialProject(t, base, p.ID)
			if err != nil {
				return // refused: fine
			}
			defer func() { _ = c.CloseNow() }()
			b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "core.health"})
			_ = c.Write(context.Background(), websocket.MessageText, b)
			rctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, _, _ = c.Read(rctx) // either an answer or a disconnect: both fine
		}()
		go func() {
			defer wg.Done()
			doJSON(t, http.MethodDelete, base+"/api/projects/"+p.ID, nil)
		}()
		wg.Wait()
		// Whatever the interleaving, the project must be closable and re-openable.
		_, _ = doJSON(t, http.MethodDelete, base+"/api/projects/"+p.ID, nil)
	}
}
