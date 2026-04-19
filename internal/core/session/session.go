package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/orchestra/orchestra/internal/llm"
)

// Session holds a persistent multi-turn conversation for one user.
type Session struct {
	ID           string
	History      []llm.Message
	CreatedAt    time.Time
	LastActivity time.Time

	mu       sync.Mutex
	cancelFn context.CancelFunc // non-nil while a turn is running
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// New creates a new session with a random ID.
func New() *Session {
	now := time.Now()
	return &Session{
		ID:           newID(),
		History:      make([]llm.Message, 0, 16),
		CreatedAt:    now,
		LastActivity: now,
	}
}

// Lock acquires the session mutex. Must be paired with Unlock.
func (s *Session) Lock() { s.mu.Lock() }

// Unlock releases the session mutex.
func (s *Session) Unlock() { s.mu.Unlock() }

// IsBusy reports whether a turn is currently running. Must be called with lock held.
func (s *Session) IsBusy() bool { return s.cancelFn != nil }

// SetCancel stores the cancel func for the running turn. Must be called with lock held.
func (s *Session) SetCancel(fn context.CancelFunc) { s.cancelFn = fn }

// ClearCancel removes the cancel func after a turn completes. Must be called with lock held.
func (s *Session) ClearCancel() { s.cancelFn = nil }

// Cancel cancels the currently running turn (no-op if idle).
func (s *Session) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancelFn != nil {
		s.cancelFn()
		s.cancelFn = nil
	}
}

// AppendHistory appends messages and updates LastActivity. Must be called with lock held.
func (s *Session) AppendHistory(msgs []llm.Message) {
	s.History = append(s.History, msgs...)
	s.LastActivity = time.Now()
}

// CopyHistory returns a shallow copy of the current history. Must be called with lock held.
func (s *Session) CopyHistory() []llm.Message {
	out := make([]llm.Message, len(s.History))
	copy(out, s.History)
	return out
}
