package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/checkpoint"
	"github.com/orchestra/orchestra/llm"
)

// sessionResumeLLM plays a session turn: it stages an edit to a.txt, then —
// while hang is set — never answers, as a model does when its core is killed
// under it; resumed, it finishes.
type sessionResumeLLM struct {
	mu    sync.Mutex
	hang  bool
	hung  chan struct{}
	once  sync.Once
	calls int
	saw   []string
}

func (s *sessionResumeLLM) Plan(context.Context, string) (string, error) { return "{}", nil }

func (s *sessionResumeLLM) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	steps := 0
	var said strings.Builder
	for _, m := range req.Messages {
		if m.Role == llm.RoleAssistant {
			steps++
		}
		if m.Role == llm.RoleUser {
			said.WriteString(m.Content)
			said.WriteString("\n")
		}
	}
	s.mu.Lock()
	s.calls++
	s.saw = append(s.saw, said.String())
	hang := s.hang
	s.mu.Unlock()
	if steps == 0 {
		return toolCall("edit", `{"path":"a.txt","search":"a0","replace":"a1"}`), nil
	}
	if hang {
		s.once.Do(func() { close(s.hung) })
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return finalText("a.txt changed"), nil
}

func awaitSessionCrashPoint(t *testing.T, root, sessionID string, client *sessionResumeLLM) (string, []byte) {
	t.Helper()
	select {
	case <-client.hung:
	case <-time.After(20 * time.Second):
		t.Fatal("the model never reached the crash point")
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		cp, err := checkpoint.LatestWhere(root, func(cp *checkpoint.Checkpoint) bool { return sessionOf(cp) == sessionID })
		if err == nil && len(cp.History) >= 2 && len(cp.Staged) == 1 {
			if data, err := os.ReadFile(checkpoint.Path(root, cp.RunID)); err == nil {
				return cp.RunID, data
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the session's checkpoint never showed the staged edit")
	return "", nil
}

// A session's turn is checkpointed like agent.run's, and session.message
// {resume} goes on from it in a new core (phase 4.6): the staged edit comes
// back, the model is told, the chat keeps one question, and the turn ends
// with the edit.
func TestSessionMessage_ResumesATurnItsCoreDidNotFinish(t *testing.T) {
	root := resumeWorkspace(t)
	first := &sessionResumeLLM{hang: true, hung: make(chan struct{})}
	c1, err := New(root, Options{LLMClient: first})
	if err != nil {
		t.Fatal(err)
	}
	start, err := c1.SessionStart(SessionStartParams{})
	if err != nil {
		t.Fatal(err)
	}
	sid := start.SessionID
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := c1.SessionMessage(ctx, SessionMessageParams{SessionID: sid, Content: "change a.txt", Mode: "build"})
		done <- err
	}()
	turnID, atCrash := awaitSessionCrashPoint(t, root, sid, first)
	// The process dies here; what it would still have done is undone.
	cancel()
	<-done
	c1.Close()
	if err := os.WriteFile(checkpoint.Path(root, turnID), atCrash, 0o600); err != nil {
		t.Fatal(err)
	}

	second := &sessionResumeLLM{hung: make(chan struct{})}
	c2, err := New(root, Options{LLMClient: second})
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	reopened, err := c2.SessionStart(SessionStartParams{SessionID: sid})
	if err != nil {
		t.Fatal(err)
	}
	if reopened.ResumableTurnID != turnID {
		t.Fatalf("session.start names the turn to resume: %q, want %q", reopened.ResumableTurnID, turnID)
	}
	if _, err := c2.SessionMessage(context.Background(), SessionMessageParams{SessionID: sid, Resume: "last", AllowExec: false}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	res, err := c2.SessionGet(SessionGetParams{SessionID: sid})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.saw) == 0 || !strings.Contains(second.saw[0], "<resume_notice>") || !strings.Contains(second.saw[0], "staged edits restored: 1") {
		t.Fatalf("the model is told the turn was resumed with its edit: %q", second.saw)
	}
	users := 0
	for _, m := range res.UIMessages {
		if m.Role == "user" {
			users++
		}
	}
	if users != 1 {
		t.Fatalf("the chat keeps one question: %d user messages", users)
	}
	sess, err := c2.sessions.GetOrLoad(root, sid)
	if err != nil {
		t.Fatal(err)
	}
	pending := false
	for _, op := range sess.CopyPending() {
		if op.Path == "a.txt" && op.WriteAtomic != nil && op.WriteAtomic.Content == "a1\n" {
			pending = true
		}
	}
	if !res.HasPending || !pending {
		t.Fatalf("the turn ends with the restored edit pending: %v %+v", res.HasPending, sess.CopyPending())
	}
	cp, err := checkpoint.Load(root, turnID)
	if err != nil || cp.Status != checkpoint.StatusDone || cp.Resumes != 1 {
		t.Fatalf("the checkpoint says the turn finished after one resume: %+v %v", cp, err)
	}
	if again, err := c2.SessionStart(SessionStartParams{SessionID: sid}); err != nil || again.ResumableTurnID != "" {
		t.Fatalf("nothing left to resume: %+v %v", again, err)
	}
	if _, err := c2.SessionMessage(context.Background(), SessionMessageParams{SessionID: sid, Resume: turnID}); err == nil {
		t.Fatal("a finished turn cannot be resumed")
	}
}

// A session resumes its own turns only; agent.run's runs are not a session's.
func TestSessionMessage_ResumesOnlyItsOwnTurns(t *testing.T) {
	root := resumeWorkspace(t)
	c, err := New(root, Options{LLMClient: &sessionResumeLLM{hung: make(chan struct{})}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := checkpoint.Save(root, &checkpoint.Checkpoint{RunID: "20260101T000000-run", ProjectRoot: root, Status: checkpoint.StatusFailed, Params: []byte(`{"query":"q"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := checkpoint.Save(root, &checkpoint.Checkpoint{RunID: "20260101T000001-other", ProjectRoot: root, Status: checkpoint.StatusFailed, Params: []byte(`{"query":"q","session_id":"other"}`)}); err != nil {
		t.Fatal(err)
	}
	start, err := c.SessionStart(SessionStartParams{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.SessionMessage(context.Background(), SessionMessageParams{SessionID: start.SessionID, Resume: "last"}); err == nil || !strings.Contains(err.Error(), "no checkpoint") {
		t.Fatalf("another session's turn and a run are not this session's: %v", err)
	}
	if _, err := c.SessionMessage(context.Background(), SessionMessageParams{SessionID: start.SessionID, Resume: "20260101T000000-run"}); err == nil || !strings.Contains(err.Error(), "agent.run") {
		t.Fatalf("a run is named as one: %v", err)
	}
	if _, err := c.AgentRun(context.Background(), AgentRunParams{Resume: "20260101T000001-other"}); err == nil || !strings.Contains(err.Error(), "belongs to session") {
		t.Fatalf("agent.run does not resume a session's turn: %v", err)
	}
	_ = filepath.Join
}
