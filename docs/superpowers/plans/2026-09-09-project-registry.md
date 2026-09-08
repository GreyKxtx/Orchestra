# Multi-project Registry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `orchestra web` holds several projects at once — one core per project, one
WebSocket per project — with an HTTP API to open, list and close them, and cookie
authentication so the browser needs no credential in JavaScript.

**Architecture:** One Go process owns a `Registry`: a mutex-guarded
`map[projectID]*entry`, where each entry is an ordinary single-workspace
`core.Core`. Nothing about `Core` changes. `/ws?project=<id>` looks the project up
and then runs the transport that already shipped in v15; the "one live connection"
rule becomes per-project because each project has its own core. The page-serving
handler sets an `HttpOnly` cookie and redirects, so `fetch` and `WebSocket`
authenticate themselves afterwards.

**Tech Stack:** Go 1.25, `net/http`, `github.com/coder/websocket v1.8.15`,
`patch/fsutil.AtomicWriteFile`; vanilla browser JS with the existing
dependency-free bundler.

**Spec:** `docs/superpowers/specs/2026-09-09-project-registry-design.md` (commit `c1fe6e7`)

---

## Global Constraints

Every task's requirements implicitly include this section.

1. **The contract in the spec is being built against RIGHT NOW, in parallel, by
   the repository owner.** Implement the paths, methods, status codes, field
   names and error strings from the spec's "The contract" section **exactly**. If
   the implementation forces any deviation, STOP and say so loudly in the task
   report — do not quietly adjust it. A silent rename costs someone else a day.

2. **`protocol.ProtocolVersion` does NOT move.** It stays 15
   (`protocol/version.go:28`). `/ws` gains a query parameter; no RPC method, no
   message shape and no notification changes. `docs/PROTOCOL.md` gets prose, not a
   version bump. Say this explicitly rather than leaving it ambiguous.

3. **`internal/core` is not modified.** The design rests on it having zero
   package-level mutable variables (verified: neither `var x = …` nor `var ( … )`
   form appears), so independent `*core.Core` instances share no hidden state. If
   a task finds itself editing `internal/core`, that is a signal the design was
   wrong — stop and report.

4. **The `initProject` extraction must leave `orchestra init` behaving
   identically.** Three existing tests call `runInit(initCmd, nil)` directly —
   `internal/cli/init_orchestra_md_test.go:122`, `:147`, and
   `internal/cli/memory_stats_test.go:41`. They must pass **UNCHANGED** after the
   extraction. If any needs editing, the extraction changed behaviour: re-examine
   it, do not paper over it.

5. **Do not undo `Pipe.Done()`.** It exists because EOF alone deadlocks:
   `jsonrpc.Server.Serve` waits for its in-flight handlers while a handler blocked
   in `Server.Request` waits on a context derived from the one the caller would
   cancel after `Serve` returns (`internal/webtransport/conn.go`,
   `internal/webtransport/server.go`). The disconnect-fails-closed behaviour
   depends on it.

6. **Loopback only.** `127.0.0.1`, as today. The WebSocket stays same-origin
   (`coder/websocket`'s `Accept` with `OriginPatterns: nil`). **No development
   origin escape hatch** — the spec rejects `--dev-origin` explicitly; the
   development loop is the production loop plus a file watcher.

7. **Cookie name and attributes are exact:**
   `orchestra=<token>; HttpOnly; SameSite=Strict; Path=/`. `Authorization: Bearer`,
   `X-Orchestra-Token` and `?token=` keep working for `curl`, scripts and tests.

8. **Project identity is `cache.ComputeProjectID(root)`** (`patch/cache/cache.go:104`),
   the `sha256:…` value the core already uses. No second identity scheme.

9. **No new dependencies.** Go: standard library plus what the root module already
   has. Node: the scripts stay dependency-free, matching `ui/web/scripts/*.mjs`.

10. **Per-task verification, by exit code, never by eyeballing output.** From the
    worktree root:
    ```bash
    go build ./... && go vet ./... \
      && go test -count=1 -timeout 300s ./... > /tmp/t-root.log 2>&1 \
      && (cd llm && go test -count=1 ./... > /tmp/t-llm.log 2>&1) \
      && (cd patch && go test -count=1 ./... > /tmp/t-patch.log 2>&1) \
      && (cd protocol && go test -count=1 ./... > /tmp/t-proto.log 2>&1) \
      && echo GREEN || echo RED
    ```
    Plus `go test -race -count=1` on touched packages. When `ui/web` is touched,
    also `node ui/web/scripts/check-web.mjs` and
    `node ui/web/scripts/adapter-test.mjs`. **`internal/core` has a known
    pre-existing flake that fails only under full-suite parallel load and passes
    in isolation — do not chase it**; re-run the named test alone to confirm.

11. **TDD, and mutation-verify every non-trivial test claim.** Write the failing
    test, RUN it, read the failure, then the minimal code. Then break the
    production line the test guards, watch *that* test fail, restore. The spec
    names the cookie authentication check as one that must be mutation-verified —
    it is what stands between a stray web page and a shell.

12. **Separate thematic commit per task.**

13. **Line endings:** the repo is CRLF-in-worktree on Windows. `git diff --stat`
    showing nothing while `git status` shows a file modified means an EOL-only
    touch — `git checkout --` it rather than committing churn.

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/projects/registry.go` (new) | The registry: open, close, get, list. Owns the map and the cores' lifetimes. Knows nothing about HTTP. |
| `internal/projects/registry_test.go` (new) | Two cores side by side, close frees, typed open failures. |
| `internal/projects/store.go` (new) | `~/.orchestra/projects.json` — load and save the open-path list. Paths only. |
| `internal/projects/store_test.go` (new) | Round trip, 0600, absent file, corrupt file. |
| `internal/webtransport/server.go` (modify) | `/ws?project=`, `/api/projects`, the cookie-setting page handler. Per-project connection guard. |
| `internal/webtransport/projects_api_test.go` (new) | The contract, endpoint by endpoint, against a real server. |
| `internal/webtransport/cookie_test.go` (new) | Cookie set on page load; cookie alone authenticates; nothing is 401. |
| `internal/cli/init_project.go` (new) | `initProject(ctx, root, opts)` — the body lifted out of `runInit`. |
| `internal/cli/init.go` (modify `:34-173`) | `runInit` becomes a thin caller. |
| `internal/cli/web.go` (modify) | Build a `Registry` instead of one core; open the startup project; restore persisted ones. |
| `ui/web/src/00-web-prelude.js` (modify) | `socketURL()` takes a project id; no token threading. |
| `ui/web/scripts/watch-web.mjs` (new) | `fs.watch` over the fragment sources, re-runs the bundler. |
| `docs/PROTOCOL.md`, `README.md`, `README.ru.md` (modify) | `?project=`, the cookie, and the explicit note that the protocol version does not move. |

---

### Task 1: The registry

The registry owns cores and their lifetimes. It is deliberately HTTP-free so it
can be tested with two real cores and no server.

**Files:**
- Create: `internal/projects/registry.go`
- Create: `internal/projects/registry_test.go`

**Interfaces:**
- Consumes: `core.New(root string, opts core.Options) (*core.Core, error)` (`internal/core/core.go:84`), `(*core.Core).Close()`, `.WarmupCKG(ctx)`, `.WarmupLSP(ctx)`; `cache.ComputeProjectID(root string) (string, error)` (`patch/cache/cache.go:104`).
- Produces:
  ```go
  // package projects
  type State string
  const (StateReady State = "ready"; StateError State = "error")

  type Project struct {
      ID       string `json:"id"`
      Path     string `json:"path"`
      Name     string `json:"name"`
      State    State  `json:"state"`
      Error    string `json:"error"`
      OpenedAt int64  `json:"opened_at"`
  }

  type Registry struct{ /* unexported */ }
  func NewRegistry(opts core.Options) *Registry
  func (r *Registry) Open(ctx context.Context, path string) (Project, error)
  func (r *Registry) Close(id string) error
  func (r *Registry) Get(id string) (*core.Core, bool)
  func (r *Registry) List() []Project
  func (r *Registry) Shutdown()

  var (
      ErrAlreadyOpen    = errors.New("already_open")
      ErrNoSuchDir      = errors.New("no_such_dir")
      ErrNotInitialized = errors.New("not_initialized")
  )
  ```
  `Open` returns `ErrAlreadyOpen` wrapped so `errors.Is` matches, and the existing
  project's ID reachable via `OpenedIDFor(path)`.

- [ ] **Step 1: Write the failing test**

Create `internal/projects/registry_test.go`:

```go
package projects

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/core"
)

// initWorkspace makes a directory the registry will accept: core.New loads
// .orchestra.yml and fails without it.
func initWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	cfg := config.DefaultConfig(root)
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	return root
}

func TestRegistry_TwoProjectsHaveIndependentCores(t *testing.T) {
	a := initWorkspace(t)
	b := initWorkspace(t)

	r := NewRegistry(core.Options{})
	t.Cleanup(r.Shutdown)

	pa, err := r.Open(context.Background(), a)
	if err != nil {
		t.Fatalf("open a: %v", err)
	}
	pb, err := r.Open(context.Background(), b)
	if err != nil {
		t.Fatalf("open b: %v", err)
	}

	if pa.ID == pb.ID {
		t.Fatalf("two workspaces got the same project id: %q", pa.ID)
	}
	ca, ok := r.Get(pa.ID)
	if !ok {
		t.Fatal("core a missing from registry")
	}
	cb, ok := r.Get(pb.ID)
	if !ok {
		t.Fatal("core b missing from registry")
	}
	if ca == cb {
		t.Fatal("both projects resolved to the SAME core — they would share sessions and MCP prompts")
	}
	if got := ca.Health().WorkspaceRoot; got != a {
		t.Fatalf("core a workspace = %q, want %q", got, a)
	}
	if got := cb.Health().WorkspaceRoot; got != b {
		t.Fatalf("core b workspace = %q, want %q", got, b)
	}
	if n := len(r.List()); n != 2 {
		t.Fatalf("List() = %d projects, want 2", n)
	}
}

func TestRegistry_OpenTwiceIsRejected(t *testing.T) {
	a := initWorkspace(t)
	r := NewRegistry(core.Options{})
	t.Cleanup(r.Shutdown)

	if _, err := r.Open(context.Background(), a); err != nil {
		t.Fatalf("first open: %v", err)
	}
	if _, err := r.Open(context.Background(), a); !errors.Is(err, ErrAlreadyOpen) {
		t.Fatalf("second open err = %v, want ErrAlreadyOpen", err)
	}
	if n := len(r.List()); n != 1 {
		t.Fatalf("a rejected re-open still changed the registry: %d entries", n)
	}
}

func TestRegistry_CloseFreesTheProject(t *testing.T) {
	a := initWorkspace(t)
	r := NewRegistry(core.Options{})
	t.Cleanup(r.Shutdown)

	p, err := r.Open(context.Background(), a)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := r.Close(p.ID); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, ok := r.Get(p.ID); ok {
		t.Fatal("core still resolvable after Close — its CKG database and language servers leak")
	}
	if n := len(r.List()); n != 0 {
		t.Fatalf("List() = %d after close, want 0", n)
	}
	// The same path can be opened again, which is what makes close useful.
	if _, err := r.Open(context.Background(), a); err != nil {
		t.Fatalf("reopen after close: %v", err)
	}
}

func TestRegistry_OpenFailuresAreTypedAndDoNotPoisonTheRegistry(t *testing.T) {
	r := NewRegistry(core.Options{})
	t.Cleanup(r.Shutdown)

	missing := filepath.Join(t.TempDir(), "definitely-not-here")
	if _, err := r.Open(context.Background(), missing); !errors.Is(err, ErrNoSuchDir) {
		t.Fatalf("missing dir err = %v, want ErrNoSuchDir", err)
	}

	bare := t.TempDir() // exists, but no .orchestra.yml
	if _, err := r.Open(context.Background(), bare); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("uninitialised dir err = %v, want ErrNotInitialized", err)
	}

	// A good project still opens afterwards: one bad path must not break the rest.
	good := initWorkspace(t)
	if _, err := r.Open(context.Background(), good); err != nil {
		t.Fatalf("open after two failures: %v", err)
	}
	if n := len(r.List()); n != 1 {
		t.Fatalf("List() = %d, want 1 — failed opens must not be listed", n)
	}
}

func TestRegistry_NameIsTheDirectoryName(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "my-repo")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfg := config.DefaultConfig(root)
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	r := NewRegistry(core.Options{})
	t.Cleanup(r.Shutdown)
	p, err := r.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if p.Name != "my-repo" {
		t.Fatalf("Name = %q, want %q", p.Name, "my-repo")
	}
	if p.State != StateReady {
		t.Fatalf("State = %q, want ready", p.State)
	}
	if p.OpenedAt == 0 {
		t.Fatal("OpenedAt was never set")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && go test ./internal/projects/ -run TestRegistry -v
```

Expected: FAIL to build — `undefined: NewRegistry`, `undefined: ErrAlreadyOpen`.

- [ ] **Step 3: Write the implementation**

Create `internal/projects/registry.go`:

```go
// Package projects holds the open projects of one Orchestra server.
//
// Each project is an ordinary single-workspace core.Core; there are simply
// several of them. This is safe because internal/core declares no package-level
// mutable state, so instances share nothing implicitly. The alternative — one
// core spanning several workspaces — would mean rewriting Core, where
// workspaceRoot is threaded through the CKG, the LSP manager, the MCP host and
// the sessions path.
package projects

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/orchestra/orchestra/internal/core"
	"github.com/orchestra/orchestra/patch/cache"
)

type State string

const (
	StateReady State = "ready"
	StateError State = "error"
)

// Project is the wire shape the HTTP API returns. Field names and JSON tags are
// part of the published contract — see the spec's "The contract".
type Project struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	Name     string `json:"name"`
	State    State  `json:"state"`
	Error    string `json:"error"`
	OpenedAt int64  `json:"opened_at"`
}

var (
	ErrAlreadyOpen    = errors.New("already_open")
	ErrNoSuchDir      = errors.New("no_such_dir")
	ErrNotInitialized = errors.New("not_initialized")
)

type entry struct {
	meta Project
	core *core.Core
}

type Registry struct {
	opts core.Options

	mu      sync.RWMutex
	byID    map[string]*entry
	byPath  map[string]string // absolute path -> id
}

func NewRegistry(opts core.Options) *Registry {
	return &Registry{
		opts:   opts,
		byID:   make(map[string]*entry),
		byPath: make(map[string]string),
	}
}

// Open builds a core for path and registers it. Warmup continues in the
// background, exactly as `orchestra web` has always done for its single
// project; a project is "ready" once its core exists.
func (r *Registry) Open(ctx context.Context, path string) (Project, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Project{}, fmt.Errorf("%w: %s", ErrNoSuchDir, path)
	}
	st, err := os.Stat(abs)
	if err != nil || !st.IsDir() {
		return Project{}, fmt.Errorf("%w: %s", ErrNoSuchDir, abs)
	}
	if _, err := os.Stat(filepath.Join(abs, ".orchestra.yml")); err != nil {
		return Project{}, fmt.Errorf("%w: %s", ErrNotInitialized, abs)
	}

	r.mu.RLock()
	_, dup := r.byPath[abs]
	r.mu.RUnlock()
	if dup {
		return Project{}, fmt.Errorf("%w: %s", ErrAlreadyOpen, abs)
	}

	id, err := cache.ComputeProjectID(abs)
	if err != nil {
		return Project{}, fmt.Errorf("project id: %w", err)
	}

	c, err := core.New(abs, r.opts)
	if err != nil {
		return Project{}, fmt.Errorf("open_failed: %w", err)
	}
	c.WarmupCKG(ctx)
	c.WarmupLSP(ctx)

	meta := Project{
		ID:       id,
		Path:     abs,
		Name:     filepath.Base(abs),
		State:    StateReady,
		OpenedAt: time.Now().Unix(),
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	// Re-check under the write lock: two concurrent Opens of the same path must
	// not both build a core.
	if _, dup := r.byPath[abs]; dup {
		_ = c.Close()
		return Project{}, fmt.Errorf("%w: %s", ErrAlreadyOpen, abs)
	}
	r.byID[id] = &entry{meta: meta, core: c}
	r.byPath[abs] = id
	return meta, nil
}

// OpenedIDFor returns the id of an already-open path, for the 409 payload.
func (r *Registry) OpenedIDFor(path string) (string, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.byPath[abs]
	return id, ok
}

// Close releases the project's core, and with it its CKG database and its
// language servers.
func (r *Registry) Close(id string) error {
	r.mu.Lock()
	e, ok := r.byID[id]
	if ok {
		delete(r.byID, id)
		delete(r.byPath, e.meta.Path)
	}
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("project_not_open: %s", id)
	}
	return e.core.Close()
}

func (r *Registry) Get(id string) (*core.Core, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.byID[id]
	if !ok {
		return nil, false
	}
	return e.core, true
}

// List returns the open projects, ordered by when they were opened so the UI
// has a stable list rather than Go's randomised map order.
func (r *Registry) List() []Project {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Project, 0, len(r.byID))
	for _, e := range r.byID {
		out = append(out, e.meta)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].OpenedAt != out[j].OpenedAt {
			return out[i].OpenedAt < out[j].OpenedAt
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// Paths returns the open project paths, for persistence.
func (r *Registry) Paths() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.byPath))
	for p := range r.byPath {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// AddErrored records a project that could not be opened, so a path the user had
// open comes back visible with its reason rather than vanishing.
func (r *Registry) AddErrored(path, reason string) Project {
	abs, _ := filepath.Abs(path)
	id, err := cache.ComputeProjectID(abs)
	if err != nil {
		id = "path:" + abs
	}
	meta := Project{
		ID:       id,
		Path:     abs,
		Name:     filepath.Base(abs),
		State:    StateError,
		Error:    reason,
		OpenedAt: time.Now().Unix(),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[id] = &entry{meta: meta}
	r.byPath[abs] = id
	return meta
}

func (r *Registry) Shutdown() {
	r.mu.Lock()
	entries := make([]*entry, 0, len(r.byID))
	for _, e := range r.byID {
		entries = append(entries, e)
	}
	r.byID = make(map[string]*entry)
	r.byPath = make(map[string]string)
	r.mu.Unlock()
	for _, e := range entries {
		if e.core != nil {
			_ = e.core.Close()
		}
	}
}
```

Note the `Get` on an errored entry returns `nil, false` because `e.core` is nil —
add that guard:

```go
	e, ok := r.byID[id]
	if !ok || e.core == nil {
		return nil, false
	}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && go test ./internal/projects/ -run TestRegistry -v
```

Expected: PASS, all five.

- [ ] **Step 5: Mutation-verify the independent-cores test**

This is the test that proves the whole design. In `Open`, replace the per-path
core with a shared one by returning the first core for every path: change
`r.byID[id] = &entry{meta: meta, core: c}` to reuse any existing entry's core —

```go
	for _, existing := range r.byID {
		c = existing.core
		break
	}
	r.byID[id] = &entry{meta: meta, core: c}
```

Run `-run TestRegistry_TwoProjectsHaveIndependentCores`; expected FAIL with "both
projects resolved to the SAME core". **Restore** and re-run to confirm PASS.

- [ ] **Step 6: Race check and full verification**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && go build ./... && go vet ./... && go test -race -count=1 ./internal/projects/... && go test -count=1 -timeout 300s ./... > /tmp/t1.log 2>&1 && echo GREEN || echo RED
```

- [ ] **Step 7: Commit**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && git add internal/projects/ && git commit -m "feat(projects): registry of open projects, one core each"
```

---

### Task 2: Persistence of the open-project list

A registry that forgets on restart is not a registry. Paths only — never tokens.

**Files:**
- Create: `internal/projects/store.go`
- Create: `internal/projects/store_test.go`

**Interfaces:**
- Consumes: `fsutil.AtomicWriteFile(path string, data []byte, perm os.FileMode) error` (`patch/fsutil/atomic.go:19`).
- Produces:
  ```go
  func StorePath() (string, error)          // ~/.orchestra/projects.json
  func LoadPaths(path string) ([]string, error)  // absent file -> nil, nil
  func SavePaths(path string, paths []string) error // 0600
  ```

- [ ] **Step 1: Write the failing test**

Create `internal/projects/store_test.go`:

```go
package projects

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestStore_RoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "projects.json")
	want := []string{`C:\a\one`, `C:\b\two`}

	if err := SavePaths(p, want); err != nil {
		t.Fatalf("SavePaths: %v", err)
	}
	got, err := LoadPaths(p)
	if err != nil {
		t.Fatalf("LoadPaths: %v", err)
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("round trip = %v, want %v", got, want)
	}

	// Windows does not model these bits; skip the assertion, not the test.
	if runtime.GOOS != "windows" {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if perm := st.Mode().Perm(); perm != 0600 {
			t.Fatalf("mode = %o, want 600", perm)
		}
	}
}

func TestStore_AbsentFileIsNotAnError(t *testing.T) {
	got, err := LoadPaths(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("a first run must not fail: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v, want empty", got)
	}
}

func TestStore_CorruptFileIsNotFatal(t *testing.T) {
	p := filepath.Join(t.TempDir(), "projects.json")
	if err := os.WriteFile(p, []byte("{not json"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := LoadPaths(p)
	if err != nil {
		t.Fatalf("a corrupt list must not stop the server from starting: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v, want empty", got)
	}
}

func TestStorePath_IsUnderTheOrchestraHome(t *testing.T) {
	p, err := StorePath()
	if err != nil {
		t.Fatalf("StorePath: %v", err)
	}
	if filepath.Base(p) != "projects.json" {
		t.Fatalf("StorePath = %q, want it to end in projects.json", p)
	}
	if filepath.Base(filepath.Dir(p)) != ".orchestra" {
		t.Fatalf("StorePath = %q, want it under ~/.orchestra", p)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && go test ./internal/projects/ -run TestStore -v
```

Expected: FAIL to build — `undefined: SavePaths`.

- [ ] **Step 3: Write the implementation**

Create `internal/projects/store.go`:

```go
package projects

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/orchestra/orchestra/patch/fsutil"
)

// storeFile is the on-disk shape. Paths only: this file must never hold a
// token, so a stray backup of it leaks nothing but directory names.
type storeFile struct {
	Projects []string `json:"projects"`
}

// StorePath is ~/.orchestra/projects.json.
func StorePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".orchestra", "projects.json"), nil
}

// LoadPaths reads the remembered project paths. An absent or unreadable list is
// an empty list, never an error: a corrupt file must not stop the server from
// starting, because then the user cannot reach the UI to fix it.
func LoadPaths(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, nil
	}
	var f storeFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, nil
	}
	return f.Projects, nil
}

func SavePaths(path string, paths []string) error {
	b, err := json.MarshalIndent(storeFile{Projects: paths}, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return fsutil.AtomicWriteFile(path, b, 0600)
}
```

- [ ] **Step 4: Run to verify it passes**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && go test ./internal/projects/ -run TestStore -v
```

Expected: PASS, all four.

- [ ] **Step 5: Mutation-verify the corrupt-file test**

In `LoadPaths`, change the `json.Unmarshal` failure branch from `return nil, nil`
to `return nil, err`. Run `-run TestStore_CorruptFileIsNotFatal`; expected FAIL
with "a corrupt list must not stop the server from starting". Restore, re-run.

- [ ] **Step 6: Full verification and commit**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && go build ./... && go vet ./... && go test -count=1 ./internal/projects/... && go test -count=1 -timeout 300s ./... > /tmp/t2.log 2>&1 && echo GREEN || echo RED
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && git add internal/projects/ && git commit -m "feat(projects): remember open projects across restarts"
```

---

### Task 3: Extract `initProject` from cobra

`POST /api/projects {"init":true}` needs `orchestra init`'s logic. Today it is
`runInit(cmd, args)`, reading `os.Getwd()` and two package-level flag variables
(`internal/cli/init.go:34-173`). `cmd` is used only to obtain a context
(`:41-42`); `args` is not used at all. Every helper it calls already takes a root
parameter.

**Files:**
- Create: `internal/cli/init_project.go`
- Modify: `internal/cli/init.go:34-173`

**Interfaces:**
- Consumes: the existing unexported helpers `ensureGitignore(root)`, `ensureLearningDirs(root)`, `ensureOrchestraMD(root, dryRun, langs)`, `reportOrchestraMD(action, foundFallback)`, `suggestLocalOverlay(root, configPath)`, `detectedLanguages(root)`, `lspServersFromInit(root)`, `applyDetectedLocalServer(cfg, srv, found)`, `runInstrument(root, dryRun)`.
- Produces:
  ```go
  type InitOptions struct {
      Instrument bool
      DryRun     bool
  }
  func initProject(ctx context.Context, root string, opts InitOptions) error
  ```

- [ ] **Step 1: Write the failing test**

Create the test in `internal/cli/init_project_test.go`:

```go
package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// initProject must work on a directory that is NOT the process's cwd — that is
// the whole point of the extraction, since the server initialises whichever path
// the user picked.
func TestInitProject_InitialisesAnArbitraryDirectory(t *testing.T) {
	root := t.TempDir()

	if err := initProject(context.Background(), root, InitOptions{}); err != nil {
		t.Fatalf("initProject: %v", err)
	}

	for _, rel := range []string{".orchestra.yml", ".gitignore", "ORCHESTRA.md"} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Fatalf("%s missing after init: %v", rel, err)
		}
	}

	// The cwd must be untouched: a server initialising a project must not
	// depend on, or change, where the process happens to be.
	cwd, _ := os.Getwd()
	if _, err := os.Stat(filepath.Join(cwd, ".orchestra.yml")); err == nil {
		t.Fatal("init wrote into the process cwd instead of the given root")
	}
}

func TestInitProject_IsIdempotent(t *testing.T) {
	root := t.TempDir()
	if err := initProject(context.Background(), root, InitOptions{}); err != nil {
		t.Fatalf("first init: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(root, ".orchestra.yml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := initProject(context.Background(), root, InitOptions{}); err != nil {
		t.Fatalf("second init: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(root, ".orchestra.yml"))
	if err != nil {
		t.Fatalf("read again: %v", err)
	}
	if string(before) != string(after) {
		t.Fatal("re-running init rewrote an existing .orchestra.yml — it must be left untouched")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && go test ./internal/cli/ -run TestInitProject -v
```

Expected: FAIL to build — `undefined: initProject`, `undefined: InitOptions`.

- [ ] **Step 3: Move the body**

Create `internal/cli/init_project.go` containing `InitOptions` and
`initProject(ctx, root, opts)`. Its body is the **current body of `runInit`
verbatim** (`internal/cli/init.go` lines 35-172) with exactly three
substitutions, and no other change:

- every `cwd` becomes `root`, and the opening `cwd, err := os.Getwd()` block is deleted;
- `initDryRun` becomes `opts.DryRun`, `initInstrument` becomes `opts.Instrument`;
- the context block

  ```go
	detectCtx := context.Background()
	if cmd != nil && cmd.Context() != nil {
		detectCtx = cmd.Context()
	}
  ```

  becomes

  ```go
	detectCtx := ctx
	if detectCtx == nil {
		detectCtx = context.Background()
	}
  ```

Then replace `runInit` in `internal/cli/init.go` with:

```go
func runInit(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}
	ctx := context.Background()
	if cmd != nil && cmd.Context() != nil {
		ctx = cmd.Context()
	}
	return initProject(ctx, cwd, InitOptions{
		Instrument: initInstrument,
		DryRun:     initDryRun,
	})
}
```

Remove imports from `init.go` that are now unused, and add the ones
`init_project.go` needs.

- [ ] **Step 4: Run the new tests AND the three existing callers**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && go test ./internal/cli/ -run "TestInitProject|OrchestraMD|MemoryStats" -v
```

Expected: PASS. **The three pre-existing tests that call `runInit(initCmd, nil)`
— `init_orchestra_md_test.go:122`, `:147`, `memory_stats_test.go:41` — must pass
without being edited.** If any needs a change, the extraction altered behaviour:
stop, re-read the diff, and fix the extraction rather than the test.

- [ ] **Step 5: Mutation-verify that the root parameter is actually honoured**

In `initProject`, replace the first use of `root` in
`configPath := filepath.Join(root, ".orchestra.yml")` with `os.Getwd()`'s result:

```go
	wd, _ := os.Getwd()
	configPath := filepath.Join(wd, ".orchestra.yml")
```

Run `-run TestInitProject_InitialisesAnArbitraryDirectory`; expected FAIL with
".orchestra.yml missing after init". Restore and re-run.

- [ ] **Step 6: Full verification and commit**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && go build ./... && go vet ./... && go test -count=1 ./internal/cli/... && go test -count=1 -timeout 300s ./... > /tmp/t3.log 2>&1 && echo GREEN || echo RED
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && git add internal/cli/init.go internal/cli/init_project.go internal/cli/init_project_test.go && git commit -m "refactor(cli): lift initProject out of the cobra command"
```

---

### Task 4: The projects HTTP API

**Files:**
- Modify: `internal/webtransport/server.go`
- Create: `internal/webtransport/projects_api_test.go`

**Interfaces:**
- Consumes: `projects.Registry` and its methods (Task 1); `projects.ErrAlreadyOpen` / `ErrNoSuchDir` / `ErrNotInitialized`; `initProject` is NOT called from here — see below.
- Produces: `Options` gains two fields:
  ```go
  // Registry, when non-nil, enables /api/projects and the ?project= form of /ws.
  Registry *projects.Registry
  // InitProject initialises a directory for POST /api/projects {"init":true}.
  // Injected rather than imported so internal/webtransport does not depend on
  // internal/cli.
  InitProject func(ctx context.Context, root string) error
  ```

- [ ] **Step 1: Write the failing test**

Create `internal/webtransport/projects_api_test.go`:

```go
package webtransport

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
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
```

Add `"os"` to the imports.

- [ ] **Step 2: Run to verify it fails**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && go test ./internal/webtransport/ -run TestAPI -v
```

Expected: FAIL to build — `unknown field Registry in struct literal`.

- [ ] **Step 3: Write the implementation**

In `internal/webtransport/server.go`, add to `Options`:

```go
	// Registry, when non-nil, enables /api/projects and the ?project= form of
	// /ws. Nil keeps the original single-core behaviour.
	Registry *projects.Registry
	// InitProject initialises a directory for POST /api/projects {"init":true}.
	// Injected rather than imported so this package does not depend on
	// internal/cli.
	InitProject func(ctx context.Context, root string) error
```

and register the routes inside `Serve`, after the `/ws` handler:

```go
	if opts.Registry != nil {
		mux.HandleFunc("/api/projects", requireToken(token, func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				writeJSONStatus(w, http.StatusOK, map[string]any{"projects": opts.Registry.List()})
			case http.MethodPost:
				handleOpenProject(ctx, w, r, opts)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		}))
		mux.HandleFunc("/api/projects/", requireToken(token, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodDelete {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			id := strings.TrimPrefix(r.URL.Path, "/api/projects/")
			if err := opts.Registry.Close(id); err != nil {
				writeJSONStatus(w, http.StatusNotFound, map[string]any{"error": "project_not_open"})
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}))
	}
```

Add the helpers at the bottom of the file:

```go
func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// handleOpenProject implements POST /api/projects. The status codes and error
// strings are the published contract — see the spec's "The contract".
func handleOpenProject(ctx context.Context, w http.ResponseWriter, r *http.Request, opts Options) {
	var req struct {
		Path string `json:"path"`
		Init bool   `json:"init"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "bad_request"})
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "bad_request"})
		return
	}

	p, err := opts.Registry.Open(ctx, req.Path)
	if err == nil {
		writeJSONStatus(w, http.StatusCreated, p)
		return
	}

	// An uninitialised directory is initialised only when asked, then reopened.
	if errors.Is(err, projects.ErrNotInitialized) && req.Init && opts.InitProject != nil {
		if ierr := opts.InitProject(ctx, req.Path); ierr != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{
				"error": "open_failed", "path": req.Path, "detail": ierr.Error(),
			})
			return
		}
		p, err = opts.Registry.Open(ctx, req.Path)
		if err == nil {
			writeJSONStatus(w, http.StatusCreated, p)
			return
		}
	}

	switch {
	case errors.Is(err, projects.ErrAlreadyOpen):
		id, _ := opts.Registry.OpenedIDFor(req.Path)
		writeJSONStatus(w, http.StatusConflict, map[string]any{"error": "already_open", "id": id})
	case errors.Is(err, projects.ErrNoSuchDir):
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"error": "no_such_dir", "path": req.Path})
	case errors.Is(err, projects.ErrNotInitialized):
		writeJSONStatus(w, http.StatusUnprocessableEntity, map[string]any{"error": "not_initialized", "path": req.Path})
	default:
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{
			"error": "open_failed", "path": req.Path, "detail": err.Error(),
		})
	}
}
```

Add `"errors"`, `"io"` and `"github.com/orchestra/orchestra/internal/projects"` to
the imports.

- [ ] **Step 4: Run to verify it passes**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && go test ./internal/webtransport/ -run TestAPI -v
```

Expected: PASS, all five.

- [ ] **Step 5: Mutation-verify the auth requirement**

Remove `requireToken(token, …)` from the `/api/projects` registration, leaving the
bare handler. Run `-run TestAPI_RequiresAuth`; expected FAIL with "unauthenticated
GET = 200, want 401". Restore and re-run.

- [ ] **Step 6: Full verification and commit**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && go build ./... && go vet ./... && go test -race -count=1 ./internal/webtransport/... && go test -count=1 -timeout 300s ./... > /tmp/t4.log 2>&1 && echo GREEN || echo RED
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && git add internal/webtransport/ && git commit -m "feat(web): /api/projects — open, list and close projects"
```

---

### Task 5: `/ws?project=` and the per-project connection guard

**Files:**
- Modify: `internal/webtransport/server.go`
- Create: `internal/webtransport/ws_project_test.go`

**Interfaces:**
- Consumes: `Options.Registry` (Task 4); `(*projects.Registry).Get(id) (*core.Core, bool)`.
- Produces: `Options` gains
  ```go
  // NewProjectHandler builds a handler for one project's core. Required when
  // Registry is set; NewHandler still serves the project-less /ws.
  NewProjectHandler func(c *core.Core) (jsonrpc.Handler, func(*jsonrpc.Server))
  ```
  The `busy atomic.Bool` becomes a per-project set.

- [ ] **Step 1: Write the failing test**

Create `internal/webtransport/ws_project_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && go test ./internal/webtransport/ -run TestWS_ -v
```

Expected: FAIL to build — `unknown field NewProjectHandler`.

- [ ] **Step 3: Write the implementation**

Replace the `var busy atomic.Bool` declaration with a per-project guard:

```go
	// One live connection per project. The core's MCP host binds to a single
	// requester (internal/core/rpc_handler.go:46-52), so a second client on the
	// SAME project would silently take over its prompts. Different projects have
	// different cores, so they do not contend — the guard is keyed, not global.
	var busyMu sync.Mutex
	busy := map[string]bool{}
	acquire := func(key string) bool {
		busyMu.Lock()
		defer busyMu.Unlock()
		if busy[key] {
			return false
		}
		busy[key] = true
		return true
	}
	release := func(key string) {
		busyMu.Lock()
		delete(busy, key)
		busyMu.Unlock()
	}
```

and rewrite the `/ws` handler's opening:

```go
	mux.HandleFunc("/ws", requireToken(token, func(w http.ResponseWriter, r *http.Request) {
		projectID := strings.TrimSpace(r.URL.Query().Get("project"))

		// Resolve the handler factory before touching the guard, so a bad
		// project id never occupies a slot.
		newHandler := opts.NewHandler
		if projectID != "" {
			if opts.Registry == nil {
				http.Error(w, "project routing is not enabled", http.StatusNotFound)
				return
			}
			c, ok := opts.Registry.Get(projectID)
			if !ok {
				http.Error(w, "project_not_open", http.StatusNotFound)
				return
			}
			if opts.NewProjectHandler == nil {
				http.Error(w, "project routing is not configured", http.StatusNotFound)
				return
			}
			newHandler = func() (jsonrpc.Handler, func(*jsonrpc.Server)) {
				return opts.NewProjectHandler(c)
			}
		}
		if newHandler == nil {
			http.Error(w, "no handler", http.StatusNotFound)
			return
		}

		guardKey := projectID // "" is the project-less default connection
		if !acquire(guardKey) {
			http.Error(w, "a client is already connected", http.StatusConflict)
			return
		}
		defer release(guardKey)

		// ... unchanged from here: websocket.Accept, SetReadLimit, connCtx,
		// NewPipe, the Pipe.Done watcher goroutine, and srv.Serve(connCtx) —
		// except that `opts.NewHandler()` becomes `newHandler()`.
```

Add `"sync"` and `"strings"` (already imported) and `"github.com/orchestra/orchestra/internal/core"` to the imports; drop `"sync/atomic"` if nothing else uses it.

- [ ] **Step 4: Run to verify it passes**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && go test ./internal/webtransport/ -v
```

Expected: PASS — the three new tests **and** every test from the v15 transport
(`TestPipe_*`, `TestServe_*`, `TestWS_RealCoreHandshakeAndSession`). The old
`TestServe_SecondConcurrentConnectionIsRejected` must still pass: with no
`project` parameter the guard key is `""`, so the project-less behaviour is
unchanged.

- [ ] **Step 5: Mutation-verify the per-project guard**

Change `guardKey := projectID` to `guardKey := ""`, making the guard global
again. Run `-run TestWS_ConnectionGuardIsPerProject`; expected FAIL with "the
guard is global, not per project". Restore and re-run.

- [ ] **Step 6: Mutation-verify the project routing**

Change `return opts.NewProjectHandler(c)` to ignore `c` and use the first
project's core:

```go
			newHandler = func() (jsonrpc.Handler, func(*jsonrpc.Server)) {
				for _, p := range opts.Registry.List() {
					if cc, ok := opts.Registry.Get(p.ID); ok {
						return opts.NewProjectHandler(cc)
					}
				}
				return opts.NewProjectHandler(c)
			}
```

Run `-run TestWS_TwoProjectsServeTheirOwnCores`; expected FAIL with "both sockets
served the same workspace". Restore and re-run.

- [ ] **Step 7: Full verification and commit**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && go build ./... && go vet ./... && go test -race -count=1 ./internal/webtransport/... && go test -count=1 -timeout 300s ./... > /tmp/t5.log 2>&1 && echo GREEN || echo RED
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && git add internal/webtransport/ && git commit -m "feat(web): route /ws by project and make the connection guard per project"
```

---

### Task 6: Cookie authentication

**Files:**
- Modify: `internal/webtransport/server.go`
- Create: `internal/webtransport/cookie_test.go`

**Interfaces:**
- Consumes: `requireToken` / `authorized` (existing, `internal/webtransport/server.go`).
- Produces: `authorized` also accepts a cookie named `orchestra`; the asset
  handler is wrapped so that a request carrying a valid token in the query sets
  the cookie and redirects to the same path without it.

- [ ] **Step 1: Write the failing test**

Create `internal/webtransport/cookie_test.go`:

```go
package webtransport

import (
	"context"
	"net/http"
	"testing"
	"testing/fstest"

	"github.com/orchestra/orchestra/protocol/jsonrpc"
)

func startAssetServer(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	base, stop, err := Serve(ctx, Options{
		Token:  "secret",
		Health: map[string]any{"status": "ok"},
		Assets: fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<h1>hi</h1>")}},
		NewHandler: func() (jsonrpc.Handler, func(*jsonrpc.Server)) { return nil, nil },
	})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	t.Cleanup(func() { _ = stop() })
	return base
}

// Loading the page with a token must hand the browser a cookie and drop the
// token from the URL, so it never lands in history or the address bar.
func TestCookie_PageLoadSetsCookieAndRedirects(t *testing.T) {
	base := startAssetServer(t)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(base + "/?token=secret")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/" {
		t.Fatalf("Location = %q, want \"/\" — the token must leave the URL", loc)
	}

	var got *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "orchestra" {
			got = c
		}
	}
	if got == nil {
		t.Fatal("no orchestra cookie was set")
	}
	if got.Value != "secret" {
		t.Fatalf("cookie value = %q, want the token", got.Value)
	}
	if !got.HttpOnly {
		t.Fatal("cookie is not HttpOnly — page scripts could read the credential")
	}
	if got.SameSite != http.SameSiteStrictMode {
		t.Fatalf("SameSite = %v, want Strict — this is the CSRF guard for /api/*", got.SameSite)
	}
	if got.Path != "/" {
		t.Fatalf("cookie Path = %q, want \"/\"", got.Path)
	}
}

// After that redirect the browser sends only the cookie. If it does not
// authenticate, the whole flow is broken.
func TestCookie_AloneAuthenticates(t *testing.T) {
	base := startAssetServer(t)

	req, _ := http.NewRequest(http.MethodGet, base+"/health", nil)
	req.AddCookie(&http.Cookie{Name: "orchestra", Value: "secret"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cookie-only request = %d, want 200", resp.StatusCode)
	}
}

func TestCookie_WrongCookieIsRejected(t *testing.T) {
	base := startAssetServer(t)

	req, _ := http.NewRequest(http.MethodGet, base+"/health", nil)
	req.AddCookie(&http.Cookie{Name: "orchestra", Value: "not-the-token"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong cookie = %d, want 401 — any page in the browser can reach 127.0.0.1", resp.StatusCode)
	}
}

func TestCookie_NoCredentialIsRejected(t *testing.T) {
	base := startAssetServer(t)
	resp, err := http.Get(base + "/health")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no credential = %d, want 401", resp.StatusCode)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && go test ./internal/webtransport/ -run TestCookie -v
```

Expected: FAIL — `TestCookie_PageLoadSetsCookieAndRedirects` gets 200 with no
cookie; `TestCookie_AloneAuthenticates` gets 401.

- [ ] **Step 3: Write the implementation**

In `authorized`, add the cookie before the query-parameter check:

```go
	if c, err := r.Cookie(cookieName); err == nil && strings.TrimSpace(c.Value) == token {
		return true
	}
```

and add the constant near the top of the file:

```go
// cookieName carries the bearer token for browser requests. The page is served
// with it as HttpOnly, so fetch() and WebSocket authenticate themselves and no
// credential is reachable from page scripts.
const cookieName = "orchestra"
```

Replace the asset registration with a wrapper:

```go
	if opts.Assets != nil {
		files := http.FileServer(http.FS(opts.Assets))
		mux.Handle("/", requireToken(token, func(w http.ResponseWriter, r *http.Request) {
			// A valid token in the query means "this is the first load": hand
			// over the cookie and bounce to the same path without it, so the
			// credential leaves the address bar and the history.
			if strings.TrimSpace(r.URL.Query().Get("token")) == token {
				http.SetCookie(w, &http.Cookie{
					Name:     cookieName,
					Value:    token,
					Path:     "/",
					HttpOnly: true,
					SameSite: http.SameSiteStrictMode,
				})
				q := r.URL.Query()
				q.Del("token")
				target := r.URL.Path
				if enc := q.Encode(); enc != "" {
					target += "?" + enc
				}
				http.Redirect(w, r, target, http.StatusFound)
				return
			}
			files.ServeHTTP(w, r)
		}))
	}
```

- [ ] **Step 4: Run to verify it passes**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && go test ./internal/webtransport/ -v
```

Expected: PASS — the four cookie tests and everything already there.

- [ ] **Step 5: Mutation-verify the cookie check (the spec names this one)**

Remove the cookie branch from `authorized`. Run `-run TestCookie_AloneAuthenticates`;
expected FAIL with "cookie-only request = 401, want 200". Restore.

Then invert it — accept any cookie value regardless of the token:

```go
	if _, err := r.Cookie(cookieName); err == nil {
		return true
	}
```

Run `-run TestCookie_WrongCookieIsRejected`; expected FAIL with "wrong cookie =
200, want 401". **Restore and re-run both.** This second mutation is the one that
matters: it is the difference between a credential and a decoration.

- [ ] **Step 6: Full verification and commit**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && go build ./... && go vet ./... && go test -race -count=1 ./internal/webtransport/... && go test -count=1 -timeout 300s ./... > /tmp/t6.log 2>&1 && echo GREEN || echo RED
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && git add internal/webtransport/ && git commit -m "feat(web): authenticate the browser with an HttpOnly cookie"
```

---

### Task 7: Wire the registry into `orchestra web`

**Files:**
- Modify: `internal/cli/web.go`
- Modify: `internal/cli/web_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1-6.
- Produces: `runWeb` builds a `projects.Registry`, opens the startup workspace
  plus every remembered path, and passes `Registry`, `NewProjectHandler` and
  `InitProject` to `webtransport.Serve`. The persisted list is saved whenever it
  changes.

- [ ] **Step 1: Write the failing test**

Append to `internal/cli/web_test.go`:

```go
func TestRestoreProjects_OpensRememberedPathsAndRecordsFailures(t *testing.T) {
	good := t.TempDir()
	cfg := config.DefaultConfig(good)
	if err := config.Save(filepath.Join(good, ".orchestra.yml"), cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	bad := filepath.Join(t.TempDir(), "was-deleted")

	reg := projects.NewRegistry(core.Options{})
	t.Cleanup(reg.Shutdown)

	restoreProjects(context.Background(), reg, []string{good, bad})

	list := reg.List()
	if len(list) != 2 {
		t.Fatalf("List() = %d, want 2 — a path that fails to open must still be listed, not dropped silently", len(list))
	}
	var sawReady, sawError bool
	for _, p := range list {
		switch p.State {
		case projects.StateReady:
			sawReady = true
			if p.Path != good {
				t.Fatalf("ready project path = %q, want %q", p.Path, good)
			}
		case projects.StateError:
			sawError = true
			if p.Error == "" {
				t.Fatal("an errored project must carry the reason it failed")
			}
		}
	}
	if !sawReady || !sawError {
		t.Fatalf("want one ready and one error, got %+v", list)
	}
}
```

Add the imports `"context"`, `"github.com/orchestra/orchestra/internal/config"`,
`"github.com/orchestra/orchestra/internal/core"`,
`"github.com/orchestra/orchestra/internal/projects"`.

- [ ] **Step 2: Run to verify it fails**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && go test ./internal/cli/ -run TestRestoreProjects -v
```

Expected: FAIL to build — `undefined: restoreProjects`.

- [ ] **Step 3: Write the implementation**

In `internal/cli/web.go`, add:

```go
// restoreProjects reopens the paths the user had open. A path that no longer
// opens is recorded as an errored project rather than dropped, so the user can
// see what happened to a project they had open instead of finding it gone.
func restoreProjects(ctx context.Context, reg *projects.Registry, paths []string) {
	for _, p := range paths {
		if _, err := reg.Open(ctx, p); err != nil {
			if errors.Is(err, projects.ErrAlreadyOpen) {
				continue
			}
			reg.AddErrored(p, err.Error())
		}
	}
}
```

and rewrite the body of `runWeb` between the core construction and
`webtransport.Serve` so that it builds a registry instead of a single core:

```go
	reg := projects.NewRegistry(core.Options{Debug: webDebug})
	defer reg.Shutdown()

	// The workspace the command was started in is the first project, and its
	// failure is still fatal: `orchestra web` in a directory that cannot be
	// opened has nothing to show.
	startup, err := reg.Open(ctx, workspace)
	if err != nil {
		return err
	}

	storePath, serr := projects.StorePath()
	if serr == nil {
		if remembered, lerr := projects.LoadPaths(storePath); lerr == nil {
			restoreProjects(ctx, reg, remembered)
		}
	}
	saveOpen := func() {
		if serr == nil {
			_ = projects.SavePaths(storePath, reg.Paths())
		}
	}
	saveOpen()
	defer saveOpen()

	startupCore, _ := reg.Get(startup.ID)

	baseURL, stop, err := webtransport.Serve(ctx, webtransport.Options{
		Addr:     fmt.Sprintf("127.0.0.1:%d", webPort),
		Token:    token,
		Health:   startupCore.Health(),
		Assets:   webui.Assets(),
		Registry: reg,
		InitProject: func(ctx context.Context, root string) error {
			return initProject(ctx, root, InitOptions{})
		},
		NewProjectHandler: func(c *core.Core) (jsonrpc.Handler, func(*jsonrpc.Server)) {
			h := core.NewRPCHandler(c)
			return h, func(srv *jsonrpc.Server) {
				h.SetNotifier(srv)
				h.SetRequester(srv.Request)
			}
		},
		NewHandler: func() (jsonrpc.Handler, func(*jsonrpc.Server)) {
			h := core.NewRPCHandler(startupCore)
			return h, func(srv *jsonrpc.Server) {
				h.SetNotifier(srv)
				h.SetRequester(srv.Request)
			}
		},
	})
```

Delete the old `c, err := core.New(...)`, its `defer c.Close()`, and the
`c.WarmupCKG` / `c.WarmupLSP` calls — the registry does the warmup now. Add
`"errors"` and `"github.com/orchestra/orchestra/internal/projects"` to the
imports.

`saveOpen` is called once after restore and again on shutdown; a project opened
or closed through the API is captured by the shutdown save. That is deliberate
and enough for v1 — the list is a convenience, not a transaction log.

- [ ] **Step 4: Run to verify it passes**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && go test ./internal/cli/ -run "TestRestoreProjects|TestWriteWebDiscovery|TestCleanupStaleDiscovery|TestWebAssets" -v
```

Expected: PASS.

- [ ] **Step 5: Mutation-verify the errored-project behaviour**

In `restoreProjects`, replace `reg.AddErrored(p, err.Error())` with `continue`.
Run `-run TestRestoreProjects`; expected FAIL with "a path that fails to open
must still be listed, not dropped silently". Restore and re-run.

- [ ] **Step 6: Smoke-test by hand**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && go build -o /tmp/ow.exe ./cmd/orchestra
rm -rf /tmp/p1 /tmp/p2 && mkdir -p /tmp/p1 /tmp/p2
cd /tmp/p1 && /tmp/ow.exe init >/dev/null 2>&1
cd /tmp/p1 && /tmp/ow.exe web --no-open --port 7799 > /tmp/reg.log 2>&1 &
```

Then, with `TOKEN` read from `/tmp/p1/.orchestra/web.json`:

```bash
curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:7799/api/projects
curl -s -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"path":"/tmp/p2","init":true}' http://127.0.0.1:7799/api/projects
curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:7799/api/projects
```

Expected: one project, then a second created with `.orchestra.yml` written, then
two listed. Record what you saw in the task report. Kill the process and clean up
`/tmp/p1 /tmp/p2 /tmp/ow.exe`.

- [ ] **Step 7: Full verification and commit**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && go build ./... && go vet ./... && go test -race -count=1 ./internal/cli/... ./internal/webtransport/... ./internal/projects/... && go test -count=1 -timeout 300s ./... > /tmp/t7.log 2>&1 && echo GREEN || echo RED
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && git add internal/cli/ && git commit -m "feat(cli): orchestra web serves a registry of projects"
```

---

### Task 8: Frontend — project-aware socket, no token threading, and a watcher

**Files:**
- Modify: `ui/web/src/00-web-prelude.js`
- Create: `ui/web/scripts/watch-web.mjs`
- Modify: `ui/web/scripts/adapter-test.mjs`
- Regenerate: `ui/web/static/web.bundle.js`

**Interfaces:**
- Consumes: `/ws?project=<id>`, the cookie flow (Tasks 5-6).
- Produces: `socketURL(projectId)` builds `${scheme}//${location.host}/ws` plus
  `?project=<id>` when a project id is known, and **no token**. `connect()`
  accepts an optional project id.

- [ ] **Step 1: Write the failing test**

Append to `ui/web/scripts/adapter-test.mjs`:

```javascript
test("the socket URL carries no token — the cookie authenticates", async () => {
  const b = loadBundle();
  assert.ok(b.socketURL, "the bundle did not expose the socket URL it dialled");
  assert.ok(
    !/token=/.test(b.socketURL),
    `socket URL still threads a token (${b.socketURL}); the cookie is the credential now`
  );
});

test("a project id reaches the socket URL", async () => {
  const b = loadBundle({ search: "?project=sha256%3Aabc" });
  assert.match(
    b.socketURL,
    /[?&]project=sha256%3Aabc/,
    `socket URL did not carry the project (${b.socketURL})`
  );
});
```

Extend `loadBundle` to take an options object and to expose the dialled URL:

```javascript
export function loadBundle(opts = {}) {
  // ... existing body, with:
  //   location: { protocol: "http:", host: "127.0.0.1:9", search: opts.search ?? "" },
  // and in the returned object:
  //   get socketURL() { return socket ? socket.url : ""; },
```

Change the existing `location` line from `search: "?token=t"` to
`search: opts.search ?? ""`, and add `socketURL` to the returned handle.

- [ ] **Step 2: Run to verify it fails**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && node ui/web/scripts/adapter-test.mjs 2>&1 | grep -E "^✖|^✔" | head -15
```

Expected: the two new tests fail — the URL still contains `token=`.

- [ ] **Step 3: Write the implementation**

In `ui/web/src/00-web-prelude.js`, replace `socketURL` and the `connect` signature:

```javascript
  /**
   * The socket for a project. No token: the page was served with an HttpOnly
   * cookie, which the browser attaches to the handshake by itself.
   * @param {string} projectId
   */
  function socketURL(projectId) {
    const scheme = location.protocol === "https:" ? "wss:" : "ws:";
    const base = `${scheme}//${location.host}/ws`;
    return projectId ? `${base}?project=${encodeURIComponent(projectId)}` : base;
  }

  /** @param {string} [projectId] */
  function connect(projectId) {
    const id = projectId || new URLSearchParams(location.search).get("project") || "";
    ws = new WebSocket(socketURL(id));
    // ... rest unchanged
```

`10-adapter-session.js` keeps calling `connect()` with no argument, which now
picks the project from the URL when there is one and otherwise uses the
project-less default — the behaviour the server preserves for exactly this case.

- [ ] **Step 4: Write the watcher**

Create `ui/web/scripts/watch-web.mjs`:

```javascript
// Re-runs the bundler when a source fragment changes.
//
// The development loop is the production loop: build into ui/web/static and let
// the Go server serve it. A separate dev server on another port would be a
// different origin, so the cookie would not be sent and the WebSocket handshake
// would fail on Origin — which is why there is no dev-server mode.
//
// Run: node ui/web/scripts/watch-web.mjs   (no dependencies)

import { execFileSync } from "child_process";
import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const root = path.join(__dirname, "..");
const repo = path.join(root, "..", "..");

const watched = [
  path.join(repo, "ui", "vscode", "media", "chat-src"),
  path.join(repo, "ui", "vscode", "media"), // chat.css
  path.join(root, "src"),
  root, // index.src.html
];

const bundle = () => {
  try {
    execFileSync(process.execPath, [path.join(__dirname, "bundle-web.mjs")], { stdio: "inherit" });
  } catch (e) {
    console.error("bundle failed:", e.message);
  }
};

bundle();

let pending = null;
const schedule = () => {
  clearTimeout(pending);
  // Editors write in bursts; one rebuild per burst is enough.
  pending = setTimeout(bundle, 120);
};

for (const dir of watched) {
  if (!fs.existsSync(dir)) continue;
  fs.watch(dir, { persistent: true }, (_event, name) => {
    if (!name) return;
    if (/\.(js|txt|css|html)$/.test(name)) schedule();
  });
}

console.log("watching for changes — Ctrl+C to stop");
```

- [ ] **Step 5: Rebundle and run everything**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && node ui/web/scripts/bundle-web.mjs && node ui/web/scripts/adapter-test.mjs 2>&1 | grep -cE "^✔" && node ui/web/scripts/check-web.mjs 2>&1 | tail -2
```

Expected: 13 passing tests, `web checks passed`.

- [ ] **Step 6: Mutation-verify the token removal**

Put the token back into `socketURL`:

```javascript
    return projectId ? `${base}?project=${projectId}&token=x` : `${base}?token=x`;
```

Rebundle, run `adapter-test.mjs`; expected FAIL with "socket URL still threads a
token". Restore, rebundle, re-run.

- [ ] **Step 7: Commit**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && git add ui/web/ && git commit -m "feat(web): dial /ws by project, with the cookie as the credential"
```

---

### Task 9: Documentation

**Files:**
- Modify: `docs/PROTOCOL.md` (the `### WebSocket (\`orchestra web\`) — v15+` subsection)
- Modify: `README.md`, `README.ru.md` (the Web UI sections)
- Modify: `ui/desktop/README.md`
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: everything above.
- Produces: no code.

- [ ] **Step 1: PROTOCOL.md**

In the WebSocket subsection, after the token forms, add:

```markdown
**Cookie.** Отдавая страницу по ссылке с `?token=`, сервер ставит
`orchestra=<token>; HttpOnly; SameSite=Strict; Path=/` и редиректит на тот же
путь без токена — чтобы тот не оседал в адресной строке и истории. Дальше
браузер сам прикладывает cookie и к `fetch`, и к WebSocket-хендшейку; странице
не нужно хранить учётные данные в JavaScript. `SameSite=Strict` закрывает CSRF
на `/api/*`, WebSocket дополнительно ограничен same-origin.

**Выбор проекта.** `GET /ws?project=<id>` подключает к конкретному проекту;
`id` — это `project_id` из `core.health` (`sha256:…`). Без параметра — проект,
в котором запущен сервер, как и раньше. Правило «одно живое соединение»
действует **на проект**: у каждого проекта своё ядро, и перехватывать чужие
MCP-промпты им нечем.

Версия протокола при этом **не меняется** (остаётся 15): `/ws` получил
query-параметр, а не новый метод или новую форму сообщения.
```

- [ ] **Step 2: READMEs**

Replace the "One tab at a time" sentence in `README.md` with:

```markdown
`orchestra web` holds several projects at once — one core each. `GET /api/projects`
lists them, `POST /api/projects {"path":…}` opens one (add `"init":true` to
initialise a repository that has no `.orchestra.yml`), `DELETE /api/projects/{id}`
closes one and frees its language servers and index. The open list is remembered
in `~/.orchestra/projects.json`.

One tab per project: a second connection to the *same* project is refused with
`409` while the first is live, because that core's MCP host binds to one client.
Different projects never contend. Closing the tab ends the session and fails any
pending permission prompt closed.
```

Write the equivalent in `README.ru.md`, matching that file's tone.

- [ ] **Step 3: `ui/desktop/README.md`**

Replace "Стек: TBD — рассматриваются Tauri и Electron" with the decision that was
actually made: Tauri, over the same `ui/web` frontend, which is why that frontend
uses no Chromium-only APIs. Note that the multi-project registry this plan builds
is part A of three, and the desktop shell and packaging are parts B and C.

- [ ] **Step 4: CI**

The `vscode-extension` job already runs `node ui/web/scripts/check-web.mjs` and
`node ui/web/scripts/adapter-test.mjs`; no change is needed there. Confirm by
reading `.github/workflows/ci.yml` and say so explicitly in the task report
rather than leaving it unverified.

- [ ] **Step 5: Verify every file:line reference in the new prose**

Line numbers moved during this work. Re-check each reference written in Steps 1-3
against the tree as it now stands, and fix any that drifted.

- [ ] **Step 6: Full verification and commit**

```bash
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && go build ./... && go vet ./... && go test -count=1 -timeout 300s ./... > /tmp/t9-root.log 2>&1; echo "root=$?"
cd "C:/Users/KorsunAndrii/Desktop/Project/Orchestra/.worktrees/feat-project-registry" && git add docs/ README.md README.ru.md ui/desktop/ && git commit -m "docs: multi-project registry, cookie auth, and the Tauri decision"
```

---

## Notes for the executor

**The contract is load-bearing and someone else is building against it today.**
Paths, methods, status codes, JSON field names and error strings come from the
spec verbatim. If something forces a change, say so in the task report in capital
letters; do not adjust quietly.

**What this plan does not build.** The session sidebar, the project picker, the
settings screens and the Tauri packaging are parts B and C. This part ends with a
working HTTP + WebSocket surface and no new pixels.

**Where it is most likely to go wrong.** Task 5 rewrites the `/ws` handler that
Task-6-through-8 of the previous plan built and mutation-verified. Every existing
`internal/webtransport` test must still pass afterwards — especially
`TestServe_DisconnectMidPermissionFailsClosed`, which guards the deadlock that
`Pipe.Done()` exists to prevent. If it starts hanging, the `Pipe.Done` watcher
goroutine was dropped in the rewrite.
