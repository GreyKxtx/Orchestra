package webtransport

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/core"
	"github.com/orchestra/orchestra/internal/projects"
	"github.com/orchestra/orchestra/protocol/jsonrpc"
)

func initWS(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	cfg := config.DefaultConfig(root)
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	return root
}

// startRegistryServer stands up the real server with a real registry.
func startRegistryServer(t *testing.T) (base string, reg *projects.Registry) {
	t.Helper()
	// Cleanups run LIFO. The first t.TempDir call registers the RemoveAll for
	// every temp dir of this test, so it must come before reg.Shutdown is
	// registered: an open core holds .orchestra/ckg.db, and on Windows a held
	// file cannot be deleted.
	_ = t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	reg = projects.NewRegistry(core.Options{})
	t.Cleanup(reg.Shutdown)

	base, stop, err := Serve(ctx, Options{
		Token:    "secret",
		Health:   map[string]any{"status": "ok"},
		Registry: reg,
		InitProject: func(ctx context.Context, root string) error {
			cfg := config.DefaultConfig(root)
			return config.Save(filepath.Join(root, ".orchestra.yml"), cfg)
		},
		NewHandler: func() (jsonrpc.Handler, func(*jsonrpc.Server)) {
			return nil, nil // unused by these tests
		},
	})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	t.Cleanup(func() { _ = stop() })
	return base, reg
}

func doJSON(t *testing.T, method, url string, body any) (int, map[string]any) {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestAPI_OpenListClose(t *testing.T) {
	base, _ := startRegistryServer(t)
	root := initWS(t)

	status, body := doJSON(t, http.MethodPost, base+"/api/projects", map[string]any{"path": root})
	if status != http.StatusCreated {
		t.Fatalf("POST status = %d, want 201 (body %v)", status, body)
	}
	id, _ := body["id"].(string)
	if id == "" {
		t.Fatalf("POST returned no id: %v", body)
	}
	if body["state"] != "ready" {
		t.Fatalf("state = %v, want ready", body["state"])
	}
	if body["name"] != filepath.Base(root) {
		t.Fatalf("name = %v, want %q", body["name"], filepath.Base(root))
	}

	status, body = doJSON(t, http.MethodGet, base+"/api/projects", nil)
	if status != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", status)
	}
	list, _ := body["projects"].([]any)
	if len(list) != 1 {
		t.Fatalf("GET returned %d projects, want 1", len(list))
	}

	status, _ = doJSON(t, http.MethodDelete, base+"/api/projects/"+id, nil)
	if status != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204", status)
	}
	_, body = doJSON(t, http.MethodGet, base+"/api/projects", nil)
	list, _ = body["projects"].([]any)
	if len(list) != 0 {
		t.Fatalf("project survived DELETE: %v", body)
	}
}

func TestAPI_TypedErrors(t *testing.T) {
	base, _ := startRegistryServer(t)

	// Missing directory.
	status, body := doJSON(t, http.MethodPost, base+"/api/projects",
		map[string]any{"path": filepath.Join(t.TempDir(), "nope")})
	if status != http.StatusNotFound || body["error"] != "no_such_dir" {
		t.Fatalf("missing dir → %d %v, want 404 no_such_dir", status, body)
	}

	// Exists but not initialised.
	bare := t.TempDir()
	status, body = doJSON(t, http.MethodPost, base+"/api/projects", map[string]any{"path": bare})
	if status != http.StatusUnprocessableEntity || body["error"] != "not_initialized" {
		t.Fatalf("bare dir → %d %v, want 422 not_initialized", status, body)
	}

	// Already open.
	root := initWS(t)
	if status, body := doJSON(t, http.MethodPost, base+"/api/projects", map[string]any{"path": root}); status != http.StatusCreated {
		t.Fatalf("first open → %d %v", status, body)
	}
	status, body = doJSON(t, http.MethodPost, base+"/api/projects", map[string]any{"path": root})
	if status != http.StatusConflict || body["error"] != "already_open" {
		t.Fatalf("re-open → %d %v, want 409 already_open", status, body)
	}
	if body["id"] == nil || body["id"] == "" {
		t.Fatalf("409 must carry the existing project's id so the client can switch to it: %v", body)
	}

	// Deleting something that is not open.
	status, body = doJSON(t, http.MethodDelete, base+"/api/projects/sha256:nope", nil)
	if status != http.StatusNotFound || body["error"] != "project_not_open" {
		t.Fatalf("DELETE unknown → %d %v, want 404 project_not_open", status, body)
	}
}

func TestAPI_InitFlagCreatesTheConfig(t *testing.T) {
	base, _ := startRegistryServer(t)
	bare := t.TempDir()

	status, body := doJSON(t, http.MethodPost, base+"/api/projects",
		map[string]any{"path": bare, "init": true})
	if status != http.StatusCreated {
		t.Fatalf("init open → %d %v, want 201", status, body)
	}
	if _, err := os.Stat(filepath.Join(bare, ".orchestra.yml")); err != nil {
		t.Fatalf("init:true did not create .orchestra.yml: %v", err)
	}
}

func TestAPI_RequiresAuth(t *testing.T) {
	base, _ := startRegistryServer(t)
	resp, err := http.Get(base + "/api/projects")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET = %d, want 401 — this API opens projects and runs shell", resp.StatusCode)
	}
}

// On loopback "site" ignores the port, so SameSite=Strict alone lets any page on
// 127.0.0.1:<other> post to /api/* with the cookie attached. The Origin header
// is what separates the served page from a stranger on another port.
func TestAPI_CrossOriginRequestIsRejected(t *testing.T) {
	base, _ := startRegistryServer(t)
	host := base[len("http://"):]

	get := func(origin string) int {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, base+"/api/projects", nil)
		req.AddCookie(&http.Cookie{Name: "orchestra", Value: "secret"})
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode
	}
	if code := get("http://127.0.0.1:1"); code != http.StatusForbidden {
		t.Fatalf("cross-origin GET = %d, want 403", code)
	}
	if code := get("http://" + host); code != http.StatusOK {
		t.Fatalf("same-origin GET = %d, want 200", code)
	}
	if code := get(""); code != http.StatusOK {
		t.Fatalf("GET without Origin (curl, scripts) = %d, want 200", code)
	}
}
