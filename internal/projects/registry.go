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
	StateReady  State = "ready"
	StateError  State = "error"
	StateClosed State = "closed"
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

// ClosedProject is the wire shape for a remembered project the core does not
// hold. It deliberately does not touch the filesystem: a remembered path on an
// unreachable share would otherwise make GET /api/projects hang, which is the
// same fragility this design set out to remove from startup. Whether the
// directory is still there is discovered when the user clicks it, and the
// error travels back on that request.
func ClosedProject(path string) (Project, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Project{}, false
	}
	abs = filepath.Clean(abs)
	id, err := cache.ComputeProjectID(abs)
	if err != nil {
		return Project{}, false
	}
	return Project{
		ID:    id,
		Path:  abs,
		Name:  filepath.Base(abs),
		State: StateClosed,
	}, true
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

	mu     sync.RWMutex
	byID   map[string]*entry
	byPath map[string]string // absolute path -> id
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

	id, err := cache.ComputeProjectID(abs)
	if err != nil {
		return Project{}, fmt.Errorf("project id: %w", err)
	}

	r.mu.RLock()
	dup := r.isOpen(abs, id)
	r.mu.RUnlock()
	if dup {
		return Project{}, fmt.Errorf("%w: %s", ErrAlreadyOpen, abs)
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
	if r.isOpen(abs, id) {
		_ = c.Close()
		return Project{}, fmt.Errorf("%w: %s", ErrAlreadyOpen, abs)
	}
	// An errored placeholder for this project (a remembered path that failed to
	// open earlier) gives way to the real thing.
	if old, ok := r.byID[id]; ok && old.core == nil {
		delete(r.byPath, old.meta.Path)
	}
	r.byID[id] = &entry{meta: meta, core: c}
	r.byPath[abs] = id
	return meta, nil
}

// isOpen reports whether a *ready* project exists for this path or id. Callers
// hold r.mu. The id is the authoritative identity — cache.ComputeProjectID
// case-folds on Windows while byPath keeps the spelling it was given — so both
// keys are consulted; an errored placeholder (nil core) does not count as open.
func (r *Registry) isOpen(abs, id string) bool {
	if e, ok := r.byID[id]; ok && e.core != nil {
		return true
	}
	if pid, ok := r.byPath[abs]; ok {
		if e, ok := r.byID[pid]; ok && e.core != nil {
			return true
		}
	}
	return false
}

// OpenedIDFor returns the id of an already-open path, for the 409 payload.
func (r *Registry) OpenedIDFor(path string) (string, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	id, _ := cache.ComputeProjectID(abs)
	r.mu.RLock()
	defer r.mu.RUnlock()
	if e, ok := r.byID[id]; ok && e.core != nil {
		return id, true
	}
	if pid, ok := r.byPath[abs]; ok {
		if e, ok := r.byID[pid]; ok && e.core != nil {
			return pid, true
		}
	}
	return "", false
}

// Close releases the project's core, and with it its CKG database and its
// language servers.
func (r *Registry) Close(id string) error {
	c, err := r.Detach(id)
	if err != nil {
		return err
	}
	if c == nil {
		return nil
	}
	return c.Close()
}

// Detach removes the project from the registry and hands its core (nil for an
// errored placeholder) to the caller WITHOUT closing it. From this point no
// Get can obtain the core, so the caller may first end whatever still uses it
// — a live connection — and only then close it. Close is Detach plus close.
func (r *Registry) Detach(id string) (*core.Core, error) {
	r.mu.Lock()
	e, ok := r.byID[id]
	if ok {
		delete(r.byID, id)
		delete(r.byPath, e.meta.Path)
	}
	r.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("project_not_open: %s", id)
	}
	return e.core, nil
}

func (r *Registry) Get(id string) (*core.Core, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.byID[id]
	if !ok || e.core == nil {
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
