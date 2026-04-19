package session_test

import (
	"context"
	"sync"
	"testing"

	"github.com/orchestra/orchestra/internal/core/session"
	"github.com/orchestra/orchestra/internal/llm"
)

func TestManager_CreateAndGet(t *testing.T) {
	m := session.NewManager()
	s := m.Create()
	if s.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	got, err := m.Get(s.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != s {
		t.Fatal("expected same session pointer")
	}
}

func TestManager_GetMissing(t *testing.T) {
	m := session.NewManager()
	_, err := m.Get("no-such-id")
	if err == nil {
		t.Fatal("expected error for missing session")
	}
}

func TestManager_Delete(t *testing.T) {
	m := session.NewManager()
	s := m.Create()
	m.Delete(s.ID)
	_, err := m.Get(s.ID)
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestSession_BusyFlag(t *testing.T) {
	m := session.NewManager()
	s := m.Create()

	s.Lock()
	if s.IsBusy() {
		t.Fatal("should not be busy before SetCancel")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.SetCancel(cancel)
	if !s.IsBusy() {
		t.Fatal("should be busy after SetCancel")
	}
	s.Unlock()

	s.Cancel()

	s.Lock()
	if s.IsBusy() {
		t.Fatal("should not be busy after Cancel")
	}
	s.Unlock()

	// Verify ctx was cancelled.
	select {
	case <-ctx.Done():
	default:
		t.Fatal("context should have been cancelled")
	}
}

func TestSession_AppendAndCopyHistory(t *testing.T) {
	m := session.NewManager()
	s := m.Create()

	msgs := []llm.Message{
		{Role: llm.RoleUser, Content: "hello"},
		{Role: llm.RoleAssistant, Content: "world"},
	}
	s.Lock()
	s.AppendHistory(msgs)
	hist := s.CopyHistory()
	s.Unlock()

	if len(hist) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(hist))
	}
	if hist[0].Content != "hello" {
		t.Fatalf("unexpected content: %q", hist[0].Content)
	}
}

func TestManager_ConcurrentIsolation(t *testing.T) {
	m := session.NewManager()
	const n = 20
	sessions := make([]*session.Session, n)
	for i := range sessions {
		sessions[i] = m.Create()
	}

	var wg sync.WaitGroup
	for i, s := range sessions {
		wg.Add(1)
		go func(idx int, sess *session.Session) {
			defer wg.Done()
			msg := llm.Message{Role: llm.RoleUser, Content: "query"}
			sess.Lock()
			sess.AppendHistory([]llm.Message{msg})
			sess.Unlock()
		}(i, s)
	}
	wg.Wait()

	for _, s := range sessions {
		s.Lock()
		h := s.CopyHistory()
		s.Unlock()
		if len(h) != 1 {
			t.Fatalf("session %s: expected 1 message, got %d", s.ID, len(h))
		}
	}
}
