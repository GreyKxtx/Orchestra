package session

import (
	"fmt"
	"sync"
	"time"

	"github.com/orchestra/orchestra/internal/sessionfile"
)

// Manager manages a concurrent-safe map of sessions indexed by ID.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

// NewManager returns an empty session manager.
func NewManager() *Manager {
	return &Manager{sessions: make(map[string]*Session)}
}

// Create creates a new session, registers it, and returns it.
func (m *Manager) Create() *Session {
	return m.CreateWithID(sessionfile.NewID())
}

// CreateWithID registers a new session under id and returns it.
func (m *Manager) CreateWithID(id string) *Session {
	s := NewWithID(id)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[s.ID] = s
	return s
}

// Get returns the session with the given ID, or an error if not found.
func (m *Manager) Get(id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, fmt.Errorf("session not found: %s", id)
	}
	return s, nil
}

// Delete removes a session by ID and closes its turn. No-op if the session
// doesn't exist.
func (m *Manager) Delete(id string) {
	m.mu.Lock()
	s := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	s.CloseTurn()
}

// IDs returns the ids of the sessions held in memory.
func (m *Manager) IDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		out = append(out, id)
	}
	return out
}

// EvictIdle drops from memory the sessions that have been idle for longer
// than maxIdle and are not in a turn: their snapshot is on disk, and the
// next GetOrLoad brings them back. A long-lived core (TUI, extension) used
// to hold every session it ever touched (DATA-11). Returns how many left.
func (m *Manager) EvictIdle(maxIdle time.Duration) int {
	m.mu.Lock()
	cutoff := time.Now().Add(-maxIdle)
	var evicted []*Session
	for id, s := range m.sessions {
		// A session whose lock is held is in use right now.
		if !s.mu.TryLock() {
			continue
		}
		idle := s.cancelFn == nil && s.LastActivity.Before(cutoff)
		s.mu.Unlock()
		if idle {
			delete(m.sessions, id)
			evicted = append(evicted, s)
		}
	}
	m.mu.Unlock()
	for _, s := range evicted {
		s.CloseTurn()
	}
	return len(evicted)
}
