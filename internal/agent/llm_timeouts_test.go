package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

func newTimeoutAgent(t *testing.T, client llm.Client, opts Options) *Agent {
	t.Helper()
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tools.NewRunner(t.TempDir(), tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	if opts.Mode == "" {
		opts.Mode = ModeBuild
	}
	ag, err := New(client, v, tr, opts)
	if err != nil {
		t.Fatal(err)
	}
	return ag
}

// hangLLM never answers: it waits for its context.
type hangLLM struct{}

func (hangLLM) Plan(context.Context, string) (string, error) { return "", nil }
func (hangLLM) Complete(ctx context.Context, _ llm.CompleteRequest) (*llm.CompleteResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// LLM-14: compaction called the model with no step timeout. A compaction
// model that stopped answering held the turn with nothing but the turn's own
// limit to end it.
func TestCompaction_IsBoundedByTheStepTimeout(t *testing.T) {
	ag := newTimeoutAgent(t, hangLLM{}, Options{LLMStepTimeout: 100 * time.Millisecond})
	hist := []llm.Message{{Role: llm.RoleUser, Content: "summarise all of this please"}}
	done := make(chan error, 1)
	go func() {
		_, err := ag.compactHistory(context.Background(), "q", hist)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a compaction model that never answered produced a summary")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("compaction was not bounded by LLMStepTimeout")
	}
}

// LLM-14: a request the client already retried — here a 429 whose
// Retry-After is longer than a step waits — was retried again by the agent:
// up to nine requests for one step. The server hears it once.
func TestAgent_DoesNotRetryWhatTheClientRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"quota exceeded"}}`))
	}))
	t.Cleanup(srv.Close)
	client := llm.NewOpenAIClient(llm.LLMConfig{Provider: "vllm", APIBase: srv.URL, Model: "m"})
	ag := newTimeoutAgent(t, client, Options{MaxSteps: 2})
	if _, _, err := ag.Run(context.Background(), nil, "hi"); err == nil {
		t.Fatal("a turn whose every request was refused succeeded")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("the provider was asked %d times for one refused step", got)
	}
}
