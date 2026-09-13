package core

import (
	"context"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/llm"
)

// An eval run with --timeout 300 took 30 minutes and wrote a two-line log:
// two memory.inject events and nothing else. It had not hung in the agent
// loop — it never reached it. mode=agent routes through classifyAgentMode,
// which called the model with context.Background(), so neither the run's
// deadline nor a cancel could reach it; the run was pinned to whatever the
// HTTP stack happened to do.
//
// This is not only an eval concern: every agent.run arrives with the caller's
// context, and a request the caller has given up on should not keep a model
// call alive behind it.
type blockingLLM struct{ entered chan struct{} }

func (b *blockingLLM) Plan(ctx context.Context, prompt string) (string, error) {
	_, _ = ctx, prompt
	return "{}", nil
}

func (b *blockingLLM) Complete(ctx context.Context, req llm.CompleteRequest) (*llm.CompleteResponse, error) {
	_ = req
	select {
	case b.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestPrepareAgentLaunch_ModeRoutingHonoursTheCallersDeadline(t *testing.T) {
	root := t.TempDir()
	client := &blockingLLM{entered: make(chan struct{}, 1)}
	c, _ := setupInitializedCore(t, root, client)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		launch, err := c.prepareAgentLaunch(ctx, agentLaunchSpec{
			Mode:      string(agent.ModeAgent),
			Query:     "this project does not compile, find the error",
			SessionID: "deadline-probe",
		})
		_ = err
		if launch != nil {
			launch.Close()
		}
	}()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("prepareAgentLaunch outlived a context that expired in 300ms: the mode " +
			"classification call is detached from the caller, so nothing can stop it")
	}

	select {
	case <-client.entered:
	default:
		t.Skip("auto-router did not call the model in this configuration; nothing to assert")
	}
}
