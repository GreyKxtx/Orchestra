package session

import (
	"strconv"
	"strings"
	"sync"
)

// Interjection is a message the user sent while a turn of theirs was running.
// The turn takes it at its next step instead of the client cancelling the
// work to start over (session.interject).
type Interjection struct {
	ID      string
	Content string
}

// Interjections holds what the user sent during one turn until the agent
// loop takes it. It has its own lock, never the session's, so the loop can
// drain it while session.interject adds to it.
type Interjections struct {
	mu      sync.Mutex
	open    bool
	seq     int
	pending []Interjection
}

// Add queues content for the turn. It refuses when the turn is past its last
// look at the buffer: the client then sends the message as the next turn.
func (b *Interjections) Add(content string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.open {
		return "", false
	}
	b.seq++
	id := "ij-" + strconv.Itoa(b.seq)
	b.pending = append(b.pending, Interjection{ID: id, Content: content})
	return id, true
}

// Drain takes what is pending. At a step boundary (final false) it also
// reopens the buffer, since a final that failed keeps the loop going. At the
// final step it either takes what is pending or, with nothing there, closes
// the buffer in the same critical section: a message can never arrive after
// the last look and be lost when the turn ends.
func (b *Interjections) Drain(final bool) []Interjection {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.pending
	b.pending = nil
	switch {
	case !final:
		b.open = true
	case len(out) == 0:
		b.open = false
	}
	return out
}

// close refuses further messages and returns the ones never taken.
func (b *Interjections) close() []Interjection {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.open = false
	out := b.pending
	b.pending = nil
	return out
}

// BeginInterjections opens a buffer for the turn that is starting and returns
// it. Must be called with the session lock held.
func (s *Session) BeginInterjections() *Interjections {
	s.interjections = &Interjections{open: true}
	return s.interjections
}

// Interject hands content to the running turn. It reports false when no turn
// is running or the running one has stopped taking messages.
func (s *Session) Interject(content string) (string, bool) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", false
	}
	s.mu.Lock()
	buf := s.interjections
	s.mu.Unlock()
	if buf == nil {
		return "", false
	}
	return buf.Add(content)
}

// EndInterjections closes the turn's buffer and returns what the turn never
// took (a cancel, an error, the step limit). Must be called without the
// session lock.
func (s *Session) EndInterjections() []Interjection {
	s.mu.Lock()
	buf := s.interjections
	s.interjections = nil
	s.mu.Unlock()
	if buf == nil {
		return nil
	}
	return buf.close()
}
