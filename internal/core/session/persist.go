// Persistence for core sessions — unified v2 snapshots via internal/sessionfile.
package session

import (
	"fmt"
	"os"

	"github.com/orchestra/orchestra/internal/llm"
	"github.com/orchestra/orchestra/internal/sessionfile"
	"github.com/orchestra/orchestra/internal/tools"
)

// Snapshot serialises the session to .orchestra/sessions/<id>.json atomically.
// Callers should hold Session.mu when building a consistent view.
func (s *Session) Snapshot(workspaceRoot string) error {
	if workspaceRoot == "" || s == nil {
		return nil
	}
	snap := s.toSnapshot()
	return sessionfile.Save(workspaceRoot, snap)
}

func (s *Session) toSnapshot() *sessionfile.Snapshot {
	snap := &sessionfile.Snapshot{
		Version:     sessionfile.Version,
		ID:          s.ID,
		Title:       s.title,
		Model:       s.model,
		CreatedAt:   s.CreatedAt,
		UpdatedAt:   s.LastActivity,
		History:     s.History,
		PendingOps:  s.pendingOps,
		Todos:       todosToFile(s.todos),
		PlanPath:    s.planPath,
		UIMessages:  s.uiMessages,
		Profile:     s.profile,
		ApplyOutput: s.applyOutput,
		CostUSD:     s.costUSD,
	}
	if snap.UIMessages == nil {
		snap.UIMessages = []sessionfile.UIMessage{}
	}
	if snap.History == nil {
		snap.History = []llm.Message{}
	}
	return snap
}

func sessionFromSnapshot(snap *sessionfile.Snapshot) *Session {
	if snap == nil {
		return nil
	}
	s := &Session{
		ID:           snap.ID,
		History:      snap.History,
		CreatedAt:    snap.CreatedAt,
		LastActivity: snap.UpdatedAt,
		pendingOps:   snap.PendingOps,
		todos:        todosFromFile(snap.Todos),
		planPath:     snap.PlanPath,
		title:        snap.Title,
		model:        snap.Model,
		uiMessages:   snap.UIMessages,
		profile:      snap.Profile,
		applyOutput:  snap.ApplyOutput,
		costUSD:      snap.CostUSD,
	}
	if s.LastActivity.IsZero() {
		s.LastActivity = s.CreatedAt
	}
	if s.History == nil {
		s.History = make([]llm.Message, 0, 16)
	}
	if s.uiMessages == nil {
		s.uiMessages = make([]sessionfile.UIMessage, 0, 8)
	}
	return s
}

// DeleteSnapshot removes the on-disk snapshot for id.
func DeleteSnapshot(workspaceRoot, id string) error {
	return sessionfile.Delete(workspaceRoot, id)
}

// LoadFromDisk reads a snapshot file and constructs a fresh Session from it.
func LoadFromDisk(workspaceRoot, id string) (*Session, error) {
	if workspaceRoot == "" || id == "" {
		return nil, fmt.Errorf("session load: workspace_root and id required")
	}
	snap, err := sessionfile.Load(workspaceRoot, id)
	if err != nil {
		return nil, err
	}
	if snap.Version != sessionfile.Version {
		return nil, fmt.Errorf("session %s: unsupported snapshot version %d (this binary expects %d)", id, snap.Version, sessionfile.Version)
	}
	return sessionFromSnapshot(snap), nil
}

// GetOrLoad returns an in-memory session if present, otherwise loads from disk.
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
	if existing, ok := m.sessions[id]; ok {
		m.mu.Unlock()
		return existing, nil
	}
	m.sessions[id] = s
	m.mu.Unlock()
	return s, nil
}

func todosToFile(items []tools.TodoItem) []sessionfile.TodoItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]sessionfile.TodoItem, len(items))
	for i, it := range items {
		out[i] = sessionfile.TodoItem{
			ID:      it.ID,
			Content: it.Content,
			Status:  string(it.Status),
		}
	}
	return out
}

func todosFromFile(items []sessionfile.TodoItem) []tools.TodoItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]tools.TodoItem, len(items))
	for i, it := range items {
		out[i] = tools.TodoItem{
			ID:      it.ID,
			Content: it.Content,
			Status:  tools.TodoStatus(it.Status),
		}
	}
	return out
}

// LoadOrCreate returns an existing session if a snapshot is present,
// otherwise creates a fresh one with the given id.
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
			fresh := NewWithID(id)
			m.mu.Lock()
			if existing, ok := m.sessions[id]; ok {
				m.mu.Unlock()
				return existing, nil
			}
			m.sessions[id] = fresh
			m.mu.Unlock()
			return fresh, nil
		}
		return nil, err
	}
	m.mu.Lock()
	if existing, ok := m.sessions[id]; ok {
		m.mu.Unlock()
		return existing, nil
	}
	m.sessions[id] = s
	m.mu.Unlock()
	return s, nil
}
