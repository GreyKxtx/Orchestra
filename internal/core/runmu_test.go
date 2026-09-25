package core

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/orchestra/orchestra/llm"
)

// gateLLM blocks Complete until release is closed, then returns a final step.
type gateLLM struct {
	release chan struct{}
	entered chan struct{} // closed on first Complete (test synchronization)
	once    sync.Once
	final   string
}

func (g *gateLLM) Complete(ctx context.Context, _ llm.CompleteRequest) (*llm.CompleteResponse, error) {
	g.once.Do(func() {
		if g.entered != nil {
			close(g.entered)
		}
	})
	select {
	case <-g.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	body := g.final
	if body == "" {
		body = `{"type":"final","final":{"patches":[]}}`
	}
	return &llm.CompleteResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: body}}, nil
}

func (g *gateLLM) Plan(_ context.Context, _ string) (string, error) { return "{}", nil }

// TestRunMu_OpsApplyRunsDuringATurn: a turn holds runMu shared, so an
// ops.apply — like a turn of another session — runs while it is in flight.
// It used to wait for the whole turn: every session of a core queued behind
// the slowest model (ARCH-5). What still waits is a change to the core's
// shared state, see TestRunMu_ConfigRefreshWaitsForTheTurn.
func TestRunMu_OpsApplyRunsDuringATurn(t *testing.T) {
	root := t.TempDir()
	release := make(chan struct{})
	entered := make(chan struct{})
	_, h := setupInitializedCore(t, root, &gateLLM{release: release, entered: entered})
	var releaseOnce sync.Once
	open := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(open)
	const slow = 20 * time.Second

	startP, _ := json.Marshal(SessionStartParams{})
	startRes, err := h.Handle(context.Background(), "session.start", startP)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := startRes.(*SessionStartResult).SessionID

	msgP, _ := json.Marshal(SessionMessageParams{SessionID: sessionID, Content: "hello"})
	msgDone := make(chan error, 1)
	go func() {
		_, err := h.Handle(context.Background(), "session.message", msgP)
		msgDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(slow):
		t.Fatal("session.message did not reach LLM Complete")
	}

	applyP, _ := json.Marshal(OpsApplyParams{Ops: nil})
	applyDone := make(chan struct{}, 1)
	go func() {
		_, _ = h.Handle(context.Background(), "ops.apply", applyP)
		close(applyDone)
	}()
	select {
	case <-applyDone:
	case <-time.After(slow):
		t.Fatal("ops.apply waited on a session's turn")
	}

	open()
	select {
	case err := <-msgDone:
		if err != nil {
			t.Fatalf("session.message: %v", err)
		}
	case <-time.After(slow):
		t.Fatal("session.message did not complete")
	}
}

// TestRunMu_ConfigRefreshWaitsForTheTurn: what changes the core's shared
// state takes runMu exclusively, so it still waits for the turns in flight
// — swapping the model or the config under a running agent would race it.
func TestRunMu_ConfigRefreshWaitsForTheTurn(t *testing.T) {
	root := t.TempDir()
	release := make(chan struct{})
	entered := make(chan struct{})
	c, h := setupInitializedCore(t, root, &gateLLM{release: release, entered: entered})
	var releaseOnce sync.Once
	open := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(open)
	const slow = 20 * time.Second

	startP, _ := json.Marshal(SessionStartParams{})
	startRes, err := h.Handle(context.Background(), "session.start", startP)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := startRes.(*SessionStartResult).SessionID
	msgP, _ := json.Marshal(SessionMessageParams{SessionID: sessionID, Content: "hello"})
	msgDone := make(chan error, 1)
	go func() {
		_, err := h.Handle(context.Background(), "session.message", msgP)
		msgDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(slow):
		t.Fatal("session.message did not reach LLM Complete")
	}

	// The exclusive lock is not available while the turn runs.
	if c.runMu.TryLock() {
		c.runMu.Unlock()
		t.Fatal("the core's exclusive lock was free during a turn: a config swap could race the agent")
	}
	open()
	select {
	case err := <-msgDone:
		if err != nil {
			t.Fatalf("session.message: %v", err)
		}
	case <-time.After(slow):
		t.Fatal("session.message did not complete")
	}
	if !c.runMu.TryLock() {
		t.Fatal("the exclusive lock is still held after the turn")
	}
	c.runMu.Unlock()
}

// TestReadOnlyListsRespondDuringTurn: agents.list and mcp.list are read-only
// and must NOT queue behind runMu while a session.message turn is in flight
// (regression: the settings UI got "rpc timeout after 15000ms: agents.list"
// whenever it was opened during a long orchestra run).
func TestReadOnlyListsRespondDuringTurn(t *testing.T) {
	root := t.TempDir()
	release := make(chan struct{})
	entered := make(chan struct{})
	_, h := setupInitializedCore(t, root, &gateLLM{release: release, entered: entered})

	startP, _ := json.Marshal(SessionStartParams{})
	startRes, err := h.Handle(context.Background(), "session.start", startP)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := startRes.(*SessionStartResult).SessionID

	msgP, _ := json.Marshal(SessionMessageParams{SessionID: sessionID, Content: "hello"})
	msgDone := make(chan error, 1)
	go func() {
		_, err := h.Handle(context.Background(), "session.message", msgP)
		msgDone <- err
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("session.message did not reach LLM Complete")
	}

	// Turn is in flight and holds runMu — the list RPCs must still answer.
	for _, method := range []string{"agents.list", "mcp.list"} {
		done := make(chan error, 1)
		go func(m string) {
			_, err := h.Handle(context.Background(), m, json.RawMessage(`{}`))
			done <- err
		}(method)
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s during turn: %v", method, err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s blocked behind runMu during an in-flight turn", method)
		}
	}

	close(release)
	select {
	case err := <-msgDone:
		if err != nil {
			t.Fatalf("session.message: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("session.message did not complete")
	}
}
