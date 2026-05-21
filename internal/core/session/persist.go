// Persistence for core sessions. C1 + C4 in architecture audit: the
// previous design kept Session entirely in memory and used a separate
// internal/sessionstore for TUI-only chat persistence — restart of
// the core subprocess lost the agent's history, pending ops, and todo
// list. This file adds JSON snapshots to .orchestra/sessions/<id>.json
// owned by core; the legacy TUI sessionstore remains for chat-only
// state until a follow-up migration consolidates the two.
package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/orchestra/orchestra/internal/fsutil"
	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/ops"
	"github.com/orchestra/orchestra/internal/tools"
)

// snapshot is the on-disk shape of a Session. Versioned so future
// schema changes can be detected at load time without forcing a full
// migration. Note: cancelFn is intentionally NOT serialised — it's a
// runtime-only handle.
type snapshot struct {
	Version      int              `json:"version"`
	ID           string           `json:"id"`
	History      []llm.Message    `json:"history"`
	CreatedAt    time.Time        `json:"created_at"`
	LastActivity time.Time        `json:"last_activity"`
	PendingOps   []ops.AnyOp      `json:"pending_ops,omitempty"`
	Todos        []tools.TodoItem `json:"todos,omitempty"`
	PlanPath     string           `json:"plan_path,omitempty"`
}

const snapshotVersion = 1

// sessionsDir returns <workspaceRoot>/.orchestra/sessions. Created on
// first snapshot.
func sessionsDir(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, ".orchestra", "sessions")
}

// snapshotPath returns the on-disk path for session id.
func snapshotPath(workspaceRoot, id string) string {
	return filepath.Join(sessionsDir(workspaceRoot), id+".json")
}

// Snapshot serialises the session to .orchestra/sessions/<id>.json
// atomically. Callers should call this after every mutation that
// changes durable state (History append, SetPending, SetTodos). Lock
// the session before calling so the snapshot is consistent.
//
// workspaceRoot must be non-empty; an empty value is a no-op (callers
// that don't know the workspace root yet shouldn't fail their request
// just because persistence is unavailable).
func (s *Session) Snapshot(workspaceRoot string) error {
	if workspaceRoot == "" {
		return nil
	}
	snap := snapshot{
		Version:      snapshotVersion,
		ID:           s.ID,
		History:      s.History,
		CreatedAt:    s.CreatedAt,
		LastActivity: s.LastActivity,
		PendingOps:   s.pendingOps,
		Todos:        s.todos,
		PlanPath:     s.planPath,
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("session snapshot marshal: %w", err)
	}
	return fsutil.AtomicWriteFile(snapshotPath(workspaceRoot, s.ID), data, 0o600)
}

// DeleteSnapshot removes the on-disk snapshot for id. Returns nil if
// the file doesn't exist (close is idempotent). Other errors (perm
// issues etc.) are surfaced so callers can log them.
func DeleteSnapshot(workspaceRoot, id string) error {
	if workspaceRoot == "" || id == "" {
		return nil
	}
	if err := os.Remove(snapshotPath(workspaceRoot, id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// LoadFromDisk reads a snapshot file and constructs a fresh Session
// from it. The returned session has no cancelFn (it's idle).
//
// Returns os.ErrNotExist when no snapshot exists for id; the caller
// can fall through to Manager.Create. Returns a parse error when the
// file exists but is unreadable — the caller should NOT silently
// drop the session in that case.
func LoadFromDisk(workspaceRoot, id string) (*Session, error) {
	if workspaceRoot == "" || id == "" {
		return nil, fmt.Errorf("session load: workspace_root and id required")
	}
	data, err := os.ReadFile(snapshotPath(workspaceRoot, id))
	if err != nil {
		return nil, err
	}
	var snap snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("session %s parse: %w", id, err)
	}
	if snap.Version != snapshotVersion {
		return nil, fmt.Errorf("session %s: unsupported snapshot version %d (this binary expects %d)", id, snap.Version, snapshotVersion)
	}
	if snap.ID == "" {
		snap.ID = id
	}
	if snap.CreatedAt.IsZero() {
		snap.CreatedAt = time.Now()
	}
	if snap.LastActivity.IsZero() {
		snap.LastActivity = snap.CreatedAt
	}
	return &Session{
		ID:           snap.ID,
		History:      snap.History,
		CreatedAt:    snap.CreatedAt,
		LastActivity: snap.LastActivity,
		pendingOps:   snap.PendingOps,
		todos:        snap.Todos,
		planPath:     snap.PlanPath,
	}, nil
}

// GetOrLoad returns an in-memory session if present, otherwise loads
// it from disk and caches it. Returns the original "session not found"
// error if neither memory nor disk has it — strict semantics for
// handlers that must reject unknown IDs (SessionMessage, op.apply
// pending lookup, etc.), distinct from LoadOrCreate's permissive
// fallback used at reconnection time.
func (m *Manager) GetOrLoad(workspaceRoot, id string) (*Session, error) {
	if id == "" {
		return nil, fmt.Errorf("session not found: %s", id)
	}
	m.mu.Lock()
	if s, ok := m.sessions[id]; ok {
		m.mu.Unlock()
		return s, nil
	}
	m.mu.Unlock()

	if workspaceRoot == "" {
		return nil, fmt.Errorf("session not found: %s", id)
	}
	s, err := LoadFromDisk(workspaceRoot, id)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("session not found: %s", id)
		}
		return nil, err
	}
	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()
	return s, nil
}

// LoadOrCreate returns an existing session if a snapshot is present,
// otherwise creates a fresh one with the given id. Used by the RPC
// handler when a client reconnects with a known session id after a
// core restart.
func (m *Manager) LoadOrCreate(workspaceRoot, id string) (*Session, error) {
	if id == "" {
		return m.Create(), nil
	}
	m.mu.Lock()
	if s, ok := m.sessions[id]; ok {
		m.mu.Unlock()
		return s, nil
	}
	m.mu.Unlock()

	s, err := LoadFromDisk(workspaceRoot, id)
	if err != nil {
		if os.IsNotExist(err) {
			// No snapshot — create a session whose ID matches the
			// requested one so the client's bookkeeping stays valid.
			fresh := &Session{
				ID:           id,
				History:      make([]llm.Message, 0, 16),
				CreatedAt:    time.Now(),
				LastActivity: time.Now(),
			}
			m.mu.Lock()
			m.sessions[id] = fresh
			m.mu.Unlock()
			return fresh, nil
		}
		return nil, err
	}
	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()
	return s, nil
}
