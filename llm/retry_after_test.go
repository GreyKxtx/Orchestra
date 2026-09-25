package llm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		h    http.Header
		want time.Duration
	}{
		{http.Header{"Retry-After": {"7"}}, 7 * time.Second},
		{http.Header{"Retry-After": {"0.5"}}, 500 * time.Millisecond},
		{http.Header{"Retry-After": {"7"}, "Retry-After-Ms": {"1200"}}, 1200 * time.Millisecond},
		{http.Header{"Retry-After": {now.Add(30 * time.Second).Format(http.TimeFormat)}}, 30 * time.Second},
		{http.Header{"Retry-After": {now.Add(-time.Minute).Format(http.TimeFormat)}}, 0},
		{http.Header{"Retry-After": {"-3"}}, 0},
		{http.Header{"Retry-After": {"soon"}}, 0},
		{nil, 0},
	} {
		if got := parseRetryAfter(tc.h, now); got != tc.want {
			t.Errorf("parseRetryAfter(%v) = %v, want %v", tc.h, got, tc.want)
		}
	}
}

func TestRetryDelay(t *testing.T) {
	asked := &StatusError{Status: 429, RetryAfter: 2 * time.Second, msg: "request failed (status 429): slow down"}
	if d, ok := retryDelay(asked, 1); !ok || d != 2*time.Second {
		t.Fatalf("Retry-After wins: %v %v", d, ok)
	}
	tooLong := &StatusError{Status: 429, RetryAfter: time.Hour}
	if _, ok := retryDelay(tooLong, 1); ok {
		t.Fatal("an hour is not waited out inside a step")
	}
	if d, ok := retryDelay(errors.New("connection reset"), 2); !ok || d != 2*llmRetryBackoff {
		t.Fatalf("no Retry-After: linear backoff, got %v", d)
	}
}

// The OpenAI-compatible client waited a fixed backoff after a 429, deaf to
// the Retry-After the server sent, and came back too early.
func TestCompleteStream_HonoursRetryAfter(t *testing.T) {
	fastRetries(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0.3")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
			return
		}
		writeAssistantSSE(w, "hello")
	}))
	t.Cleanup(srv.Close)

	c := NewOpenAIClient(LLMConfig{Provider: "vllm", APIBase: srv.URL, Model: "m"})
	start := time.Now()
	resp, err := c.Complete(context.Background(), CompleteRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil || resp.Message.Content != "hello" {
		t.Fatalf("Complete: %v %+v", err, resp)
	}
	if waited := time.Since(start); waited < 300*time.Millisecond {
		t.Fatalf("the retry came %v after a 429 that asked for 300ms", waited)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls=%d", calls.Load())
	}
}

// A server that asks for longer than a step should wait is not waited for,
// and not asked again: the error goes up marked as retried, so the agent
// does not retry it either.
func TestCompleteStream_RetryAfterTooLongGoesUp(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"quota"}}`))
	}))
	t.Cleanup(srv.Close)

	c := NewOpenAIClient(LLMConfig{Provider: "vllm", APIBase: srv.URL, Model: "m"})
	_, err := c.CompleteStream(context.Background(), CompleteRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err == nil || !AlreadyRetried(err) || calls.Load() != 1 {
		t.Fatalf("err=%v retried=%v calls=%d", err, AlreadyRetried(err), calls.Load())
	}
	if extractStatusCode(err.Error()) != 429 {
		t.Fatalf("the status still reads from the message: %v", err)
	}
}

// Retries the client ran out of are marked: the agent used to retry them
// again, up to nine requests for one step.
func TestCompleteStream_ExhaustedRetriesAreMarked(t *testing.T) {
	fastRetries(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	c := NewOpenAIClient(LLMConfig{Provider: "vllm", APIBase: srv.URL, Model: "m"})
	_, err := c.CompleteStream(context.Background(), CompleteRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err == nil || !AlreadyRetried(err) || calls.Load() != int32(llmRetryAttempts) {
		t.Fatalf("err=%v retried=%v calls=%d", err, AlreadyRetried(err), calls.Load())
	}
}

func anthropicSSE(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, "event: content_block_delta\n"+
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"`+text+`"}}`+"\n"+
		"event: message_delta\n"+
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}`+"\n"+
		"event: message_stop\n"+
		`data: {"type":"message_stop"}`+"\n")
}

// The Anthropic client did not retry at all: one 529 "overloaded" failed the
// step.
func TestAnthropic_RetriesOverloaded(t *testing.T) {
	fastRetries(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(529)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`))
			return
		}
		anthropicSSE(w, "hello")
	}))
	t.Cleanup(srv.Close)

	c := NewAnthropicClient(LLMConfig{APIBase: srv.URL, APIKey: "k", Model: "claude-x"})
	resp, err := c.Complete(context.Background(), CompleteRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil || resp.Message.Content != "hello" || calls.Load() != 2 {
		t.Fatalf("err=%v resp=%+v calls=%d", err, resp, calls.Load())
	}
}

// The Anthropic stream had no watchdog: a connection that stopped sending
// held the step until its timeout.
func TestAnthropic_StallWatchdog(t *testing.T) {
	prevStall := streamStallTimeout
	streamStallTimeout = 150 * time.Millisecond
	t.Cleanup(func() { streamStallTimeout = prevStall })
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: content_block_delta\n"+
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}`+"\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)

	c := NewAnthropicClient(LLMConfig{APIBase: srv.URL, APIKey: "k", Model: "claude-x", TimeoutS: 1})
	// On failure the stream is still open: cancelling closes the connection,
	// or srv.Close waits for it forever.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := c.CompleteStream(ctx, CompleteRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatal("stream closed without a stall error")
			}
			if ev.Kind == StreamEventError {
				if !strings.Contains(ev.Err.Error(), "stream stalled") || !IsTransientLLMError(ev.Err) {
					t.Fatalf("want a retryable stall error, got %v", ev.Err)
				}
				return
			}
		case <-deadline:
			t.Fatal("the watchdog did not fire within 5s")
		}
	}
}
