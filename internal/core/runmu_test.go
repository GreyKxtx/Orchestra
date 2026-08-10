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

// TestRunMu_SessionMessageBlocksOpsApply verifies session.message holds runMu
// for the whole turn so concurrent ops.apply cannot interleave staging setup.
func TestRunMu_SessionMessageBlocksOpsApply(t *testing.T) {
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

	msgP, _ := json.Marshal(SessionMessageParams{
		SessionID: sessionID,
		Content:   "hello",
	})
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

	applyP, _ := json.Marshal(OpsApplyParams{Ops: nil})
	applyDone := make(chan struct{}, 1)
	go func() {
		_, _ = h.Handle(context.Background(), "ops.apply", applyP)
		close(applyDone)
	}()

	deadline := time.After(300 * time.Millisecond)
	select {
	case <-deadline:
		select {
		case <-applyDone:
			t.Fatal("ops.apply finished while session.message still held runMu")
		default:
		}
	case err := <-msgDone:
		t.Fatalf("session.message returned early: %v", err)
	case <-applyDone:
		t.Fatal("ops.apply finished while session.message still held runMu")
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

	select {
	case <-applyDone:
	case <-time.After(2 * time.Second):
		t.Fatal("ops.apply should complete after session.message releases runMu")
	}
}
