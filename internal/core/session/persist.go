// Persistence for core sessions — unified v2 snapshots via internal/sessionfile.
package session

import (
	"fmt"
	"os"

	"github.com/orchestra/orchestra/llm"
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
	if err := sessionfile.Save(workspaceRoot, snap); err != nil {
		return err
	}
	// Save stamps snap.UpdatedAt with the write time; remember it so
	// RefreshFromDiskIfNewer never treats our own write as external.
	s.lastSnapshotAt = snap.UpdatedAt
	return nil
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
		// Turn boundaries and fork lineage are pure carry-through: dropping
		// either here would silently erase it on the session's next save,
		// which for turn boundaries means fork and rewind lose their only
		// mapping between the UI projection and the LLM history.
		TurnStarts:      s.turnStarts,
		ParentID:        s.parentID,
		ForkedFromIndex: s.forkedFromIndex,
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
		ID:              snap.ID,
		History:         snap.History,
		CreatedAt:       snap.CreatedAt,
		LastActivity:    snap.UpdatedAt,
		pendingOps:      snap.PendingOps,
		todos:           todosFromFile(snap.Todos),
		planPath:        snap.PlanPath,
		title:           snap.Title,
		model:           snap.Model,
		uiMessages:      snap.UIMessages,
		profile:         snap.Profile,
		applyOutput:     snap.ApplyOutput,
		costUSD:         snap.CostUSD,
		turnStarts:      snap.TurnStarts,
		parentID:        snap.ParentID,
		forkedFromIndex: snap.ForkedFromIndex,
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
	s.lastSnapshotAt = snap.UpdatedAt
	return s
}

// DeleteSnapshot removes the on-disk snapshot for id (and its turn lock).
func DeleteSnapshot(workspaceRoot, id string) error {
	_ = os.Remove(turnLockPath(workspaceRoot, id))
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

// RefreshFromDiskIfNewer replaces the in-memory session state with the
// on-disk snapshot when another process wrote a newer version — the typical
// case is a detached background core that finished an agent turn after this
// core already loaded the session. No-op when the session is not cached,
// is busy with a local turn, or the disk version is not newer than what we
// last synced with. Returns true when the state was refreshed.
func (m *Manager) RefreshFromDiskIfNewer(workspaceRoot, id string) bool {
	if workspaceRoot == "" || id == "" {
		return false
	}
	m.mu.Lock()
	s, ok := m.sessions[id]
	m.mu.Unlock()
	if !ok {
		// Not cached — the next GetOrLoad reads the latest snapshot anyway.
		return false
	}
	snap, err := sessionfile.Load(workspaceRoot, id)
	if err != nil || snap.Version != sessionfile.Version {
		return false
	}

	s.Lock()
	defer s.Unlock()
	if s.IsBusy() {
		// Never clobber a running local turn.
		return false
	}
	if !snap.UpdatedAt.After(s.lastSnapshotAt) {
		return false
	}
	fresh := sessionFromSnapshot(snap)
	s.History = fresh.History
	s.CreatedAt = fresh.CreatedAt
	s.LastActivity = fresh.LastActivity
	s.pendingOps = fresh.pendingOps
	s.todos = fresh.todos
	s.planPath = fresh.planPath
	s.title = fresh.title
	s.model = fresh.model
	s.uiMessages = fresh.uiMessages
	s.profile = fresh.profile
	s.applyOutput = fresh.applyOutput
	s.costUSD = fresh.costUSD
	// Turn boundaries index into the history replaced just above, so they
	// must travel with it or the next fork/rewind cuts in the wrong place.
	s.turnStarts = fresh.turnStarts
	s.parentID = fresh.parentID
	s.forkedFromIndex = fresh.forkedFromIndex
	s.lastSnapshotAt = fresh.lastSnapshotAt
	return true
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
