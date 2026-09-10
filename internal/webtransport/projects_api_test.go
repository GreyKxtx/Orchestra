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

// startRegistryServer stands up the real server with a real registry. `known`
// may be nil, which is the open-only behaviour part A shipped.
func startRegistryServer(t *testing.T, known *projects.Store) (base string, reg *projects.Registry) {
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
		Known:    known,
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
	base, _ := startRegistryServer(t, nil)
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
	base, _ := startRegistryServer(t, nil)

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
	base, _ := startRegistryServer(t, nil)
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
	base, _ := startRegistryServer(t, nil)
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
	base, _ := startRegistryServer(t, nil)
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

func TestAPI_ListsRememberedProjectsAsClosed(t *testing.T) {
	closedDir := initWS(t)
	openDir := initWS(t)

	store, err := projects.NewStore(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.Add(closedDir); err != nil {
		t.Fatalf("Add: %v", err)
	}

	base, _ := startRegistryServer(t, store)

	if st, body := doJSON(t, "POST", base+"/api/projects", map[string]any{"path": openDir}); st != http.StatusCreated {
		t.Fatalf("open %s: status %d, body %v", openDir, st, body)
	}

	st, body := doJSON(t, "GET", base+"/api/projects", nil)
	if st != http.StatusOK {
		t.Fatalf("GET status %d, body %v", st, body)
	}
	list, _ := body["projects"].([]any)
	if len(list) != 2 {
		t.Fatalf("want 2 projects (one open, one closed), got %d: %v", len(list), body)
	}

	first, _ := list[0].(map[string]any)
	second, _ := list[1].(map[string]any)
	if first["state"] != "ready" {
		t.Fatalf("first entry state is %v; open projects must come first", first["state"])
	}
	if second["state"] != "closed" {
		t.Fatalf("second entry state is %v, want \"closed\"", second["state"])
	}
	if second["path"] != filepath.Clean(closedDir) {
		t.Fatalf("closed entry path is %v, want %q", second["path"], filepath.Clean(closedDir))
	}
	if second["opened_at"] != float64(0) {
		t.Fatalf("closed entry opened_at is %v, want 0 — it must not sort among open projects", second["opened_at"])
	}
}

func TestAPI_AnOpenProjectIsNotListedTwice(t *testing.T) {
	dir := initWS(t)

	store, err := projects.NewStore(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	// Remembered AND open — the common case, and the one that would duplicate.
	if err := store.Add(dir); err != nil {
		t.Fatalf("Add: %v", err)
	}

	base, _ := startRegistryServer(t, store)
	if st, body := doJSON(t, "POST", base+"/api/projects", map[string]any{"path": dir}); st != http.StatusCreated {
		t.Fatalf("open: status %d, body %v", st, body)
	}

	_, body := doJSON(t, "GET", base+"/api/projects", nil)
	list, _ := body["projects"].([]any)
	if len(list) != 1 {
		t.Fatalf("want 1 project, got %d: %v", len(list), body)
	}
	entry, _ := list[0].(map[string]any)
	if entry["state"] != "ready" {
		t.Fatalf("state is %v, want \"ready\" — open outranks remembered", entry["state"])
	}
}

func TestAPI_CloseKeepsThePathRemembered(t *testing.T) {
	dir := initWS(t)

	store, err := projects.NewStore(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	base, _ := startRegistryServer(t, store)

	st, body := doJSON(t, "POST", base+"/api/projects", map[string]any{"path": dir})
	if st != http.StatusCreated {
		t.Fatalf("open: status %d, body %v", st, body)
	}
	id, _ := body["id"].(string)
	if id == "" {
		t.Fatalf("open returned no id: %v", body)
	}

	if st, body := doJSON(t, "DELETE", base+"/api/projects/"+id, nil); st != http.StatusNoContent {
		t.Fatalf("close: status %d, body %v", st, body)
	}

	found := false
	for _, p := range store.Paths() {
		if p == filepath.Clean(dir) {
			found = true
		}
	}
	if !found {
		t.Fatalf("closing dropped %q from the remembered list %v; closing and forgetting are different actions", dir, store.Paths())
	}
}

func TestAPI_ForgetRemovesAProjectThatWasNeverOpened(t *testing.T) {
	gone := initWS(t)

	store, err := projects.NewStore(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.Add(gone); err != nil {
		t.Fatalf("Add: %v", err)
	}
	base, _ := startRegistryServer(t, store)

	p, ok := projects.ClosedProject(gone)
	if !ok {
		t.Fatal("ClosedProject refused a real directory")
	}
	// The registry has never held it, so Detach fails — forget must still work.
	if st, body := doJSON(t, "DELETE", base+"/api/projects/"+p.ID+"?forget=1", nil); st != http.StatusNoContent {
		t.Fatalf("forget: status %d, body %v", st, body)
	}

	for _, remembered := range store.Paths() {
		if remembered == filepath.Clean(gone) {
			t.Fatalf("forget left %q in the list: %v", gone, store.Paths())
		}
	}
}

func TestAPI_ForgetAnUnknownIDIs404(t *testing.T) {
	store, err := projects.NewStore(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	base, _ := startRegistryServer(t, store)

	if st, body := doJSON(t, "DELETE", base+"/api/projects/nobody-knows-this?forget=1", nil); st != http.StatusNotFound {
		t.Fatalf("status %d, body %v; an id neither the registry nor the list knows is a 404", st, body)
	}
}

func TestAPI_OpeningRemembers(t *testing.T) {
	dir := initWS(t)

	store, err := projects.NewStore(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	base, _ := startRegistryServer(t, store)

	if st, body := doJSON(t, "POST", base+"/api/projects", map[string]any{"path": dir}); st != http.StatusCreated {
		t.Fatalf("open: status %d, body %v", st, body)
	}

	found := false
	for _, p := range store.Paths() {
		if p == filepath.Clean(dir) {
			found = true
		}
	}
	if !found {
		t.Fatalf("opening %q did not remember it (%v); the rail would lose it on restart", dir, store.Paths())
	}
}
