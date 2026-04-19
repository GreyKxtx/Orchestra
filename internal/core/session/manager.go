package session

import (
	"fmt"
	"sync"
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
	s := New()
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

// Delete removes a session by ID. No-op if the session doesn't exist.
func (m *Manager) Delete(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
}
