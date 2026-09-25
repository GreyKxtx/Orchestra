package session_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/core/session"
	"github.com/orchestra/orchestra/llm"
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
	if !s.IsBusy() {
		t.Fatal("should still be busy after Cancel until ClearCancel (turn unwinding)")
	}
	s.ClearCancel()
	if s.IsBusy() {
		t.Fatal("should not be busy after ClearCancel")
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

func TestSession_ReplaceHistory(t *testing.T) {
	m := session.NewManager()
	s := m.Create()
	s.Lock()
	s.AppendHistory([]llm.Message{
		{Role: llm.RoleUser, Content: "a"},
		{Role: llm.RoleAssistant, Content: "b"},
		{Role: llm.RoleUser, Content: "c"},
	})
	// Compaction rewrite: shorter prefix checkpoint.
	s.ReplaceHistory([]llm.Message{
		{Role: llm.RoleUser, Content: "[Session checkpoint — structured summary]\n\nGoal: done"},
		{Role: llm.RoleAssistant, Content: "ok"},
	})
	hist := s.CopyHistory()
	s.Unlock()
	if len(hist) != 2 {
		t.Fatalf("after replace: len=%d want 2", len(hist))
	}
	if !strings.Contains(hist[0].Content, "Session checkpoint") {
		t.Fatalf("expected checkpoint, got %q", hist[0].Content)
	}
	s.Lock()
	s.ReplaceHistory(nil)
	if len(s.CopyHistory()) != 0 {
		t.Fatal("nil replace should clear history")
	}
	s.Unlock()
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

// EvictIdle drops the sessions idle past the limit and keeps the ones in a
// turn or recently used; a dropped session comes back from its snapshot.
func TestManager_EvictIdle_DropsIdleKeepsBusyAndRecent(t *testing.T) {
	m := session.NewManager()
	idle := m.CreateWithID("idle")
	idle.LastActivity = time.Now().Add(-2 * time.Hour)
	busy := m.CreateWithID("busy")
	busy.LastActivity = time.Now().Add(-2 * time.Hour)
	busy.Lock()
	busy.SetCancel(func() {})
	busy.Unlock()
	recent := m.CreateWithID("recent")

	if n := m.EvictIdle(time.Hour); n != 1 {
		t.Fatalf("evicted %d, want 1", n)
	}
	if _, err := m.Get("idle"); err == nil {
		t.Fatal("the idle session is still in memory")
	}
	if _, err := m.Get("busy"); err != nil {
		t.Fatal("the busy session was evicted mid-turn")
	}
	if _, err := m.Get("recent"); err != nil {
		t.Fatal("the recent session was evicted")
	}
	_ = recent
	ids := m.IDs()
	if len(ids) != 2 {
		t.Fatalf("IDs = %v", ids)
	}
}
