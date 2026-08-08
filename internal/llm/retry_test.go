package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/config"
)

func fastRetries(t *testing.T) {
	t.Helper()
	prevBackoff := llmRetryBackoff
	llmRetryBackoff = time.Millisecond
	t.Cleanup(func() { llmRetryBackoff = prevBackoff })
}

const okChatResponse = `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`

func TestComplete_RetriesOn5xx(t *testing.T) {
	fastRetries(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":{"message":"bad gateway"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(okChatResponse))
	}))
	t.Cleanup(srv.Close)

	c := NewOpenAIClient(config.LLMConfig{Provider: "vllm", APIBase: srv.URL, Model: "m"})
	resp, err := c.Complete(context.Background(), CompleteRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Message.Content != "ok" {
		t.Fatalf("content=%q", resp.Message.Content)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("calls=%d, want 3", got)
	}
}

func TestComplete_NoRetryOn400(t *testing.T) {
	fastRetries(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid role"}}`))
	}))
	t.Cleanup(srv.Close)

	c := NewOpenAIClient(config.LLMConfig{Provider: "vllm", APIBase: srv.URL, Model: "m"})
	_, err := c.Complete(context.Background(), CompleteRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls=%d, want 1 (400 must not be retried)", got)
	}
}

func TestComplete_ContextOverflowAutoFix(t *testing.T) {
	fastRetries(t)
	var calls atomic.Int32
	var secondMaxTokens atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		var req struct {
			MaxTokens int `json:"max_tokens"`
		}
		_ = json.Unmarshal(body, &req)
		if n == 1 {
			w.WriteHeader(http.StatusBadRequest)
			msg := "This model's maximum context length is 51200 tokens. However, you requested 53344 tokens (45152 in the messages, 8192 in the completion). Please reduce the length of the messages or completion."
			_, _ = w.Write([]byte(fmt.Sprintf(`{"error":{"message":%q}}`, msg)))
			return
		}
		secondMaxTokens.Store(int64(req.MaxTokens))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(okChatResponse))
	}))
	t.Cleanup(srv.Close)

	c := NewOpenAIClient(config.LLMConfig{Provider: "vllm", APIBase: srv.URL, Model: "m", MaxTokens: 8192})
	resp, err := c.Complete(context.Background(), CompleteRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Message.Content != "ok" {
		t.Fatalf("content=%q", resp.Message.Content)
	}
	// room = 51200 - 45152 - 2048 = 4000
	if got := secondMaxTokens.Load(); got != 4000 {
		t.Fatalf("retry max_tokens=%d, want 4000", got)
	}
	if c.ContextTokens() != 51200 {
		t.Fatalf("contextTokens=%d, want 51200 (learned from error)", c.ContextTokens())
	}
}

func TestParseContextLengthError(t *testing.T) {
	msg := "request failed (status 400): This model's maximum context length is 40960 tokens. However, you requested 45000 tokens (40000 in the messages, 5000 in the completion)."
	ctxLen, promptTok, ok := parseContextLengthError(msg)
	if !ok || ctxLen != 40960 || promptTok != 40000 {
		t.Fatalf("legacy: got ctx=%d prompt=%d ok=%v", ctxLen, promptTok, ok)
	}

	msg2 := "This model's maximum context length is 51200 tokens. However, you requested 12054 output tokens and your prompt contains at least 40000 input tokens, for a total of at least 52054 tokens."
	ctxLen, promptTok, ok = parseContextLengthError(msg2)
	if !ok || ctxLen != 51200 || promptTok != 40000 {
		t.Fatalf("out+in: got ctx=%d prompt=%d ok=%v", ctxLen, promptTok, ok)
	}

	msg3 := "This model's maximum context length is 51200 tokens. However, you requested 12054 output tokens and your prompt is large, for a total of at least 52054 tokens."
	ctxLen, promptTok, ok = parseContextLengthError(msg3)
	if !ok || ctxLen != 51200 || promptTok != 52054-12054 {
		t.Fatalf("total-only: got ctx=%d prompt=%d ok=%v", ctxLen, promptTok, ok)
	}

	msg4 := "You passed 4 input tokens and requested 2045 output tokens. However, the model's context length is only 2048 tokens, resulting in a maximum input length of 3 tokens."
	ctxLen, promptTok, ok = parseContextLengthError(msg4)
	if !ok || ctxLen != 2048 || promptTok != 4 {
		t.Fatalf("passed: got ctx=%d prompt=%d ok=%v", ctxLen, promptTok, ok)
	}

	if _, _, ok := parseContextLengthError("some other error"); ok {
		t.Fatal("must not match unrelated errors")
	}
}

func TestComplete_ContextOverflowAutoFix_NewVLLMWording(t *testing.T) {
	fastRetries(t)
	var calls atomic.Int32
	var secondMaxTokens atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		var req struct {
			MaxTokens int `json:"max_tokens"`
		}
		_ = json.Unmarshal(body, &req)
		if n == 1 {
			w.WriteHeader(http.StatusBadRequest)
			msg := "This model's maximum context length is 51200 tokens. However, you requested 12054 output tokens and your prompt contains at least 40000 input tokens, for a total of at least 52054 tokens. Please reduce the length of the input prompt or the number of requested output tokens."
			_, _ = w.Write([]byte(fmt.Sprintf(`{"error":{"message":%q}}`, msg)))
			return
		}
		secondMaxTokens.Store(int64(req.MaxTokens))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(okChatResponse))
	}))
	t.Cleanup(srv.Close)

	c := NewOpenAIClient(config.LLMConfig{Provider: "vllm", APIBase: srv.URL, Model: "m", MaxTokens: 8192})
	c.SetContextTokens(51200)
	resp, err := c.Complete(context.Background(), CompleteRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Message.Content != "ok" {
		t.Fatalf("content=%q", resp.Message.Content)
	}
	// room = 51200 - 40000 - 2048 = 9152
	if got := secondMaxTokens.Load(); got != 9152 {
		t.Fatalf("retry max_tokens=%d, want 9152", got)
	}
}

func TestIsTransientLLMError(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{fmt.Errorf("request failed (status 502): bad gateway"), true},
		{fmt.Errorf("request failed (status 429): rate limited"), true},
		{fmt.Errorf("request failed (status 400): invalid"), false},
		{fmt.Errorf("failed to send request: read tcp: connection reset by peer"), true},
		{fmt.Errorf("stream stalled: no data from server for 2m0s"), true},
		{fmt.Errorf("SSE read error: unexpected EOF"), true},
		{fmt.Errorf("SSE read error: %w", context.DeadlineExceeded), true},
		{fmt.Errorf("SSE read error: %w", context.Canceled), true},
		{fmt.Errorf("timeout awaiting response headers"), true},
		{fmt.Errorf("no choices in response"), true},
		{context.Canceled, false},
		{context.DeadlineExceeded, false},
		{nil, false},
	}
	for _, tc := range cases {
		if got := IsTransientLLMError(tc.err); got != tc.want {
			t.Errorf("IsTransientLLMError(%v)=%v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestCompleteStream_SetupRetryOn503(t *testing.T) {
	fastRetries(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"message":"loading model"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
		fl.Flush()
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		fl.Flush()
	}))
	t.Cleanup(srv.Close)

	c := NewOpenAIClient(config.LLMConfig{Provider: "vllm", APIBase: srv.URL, Model: "m"})
	ch, err := c.CompleteStream(context.Background(), CompleteRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	var content strings.Builder
	var done bool
	for ev := range ch {
		switch ev.Kind {
		case StreamEventMessageDelta:
			content.WriteString(ev.Content)
		case StreamEventDone:
			done = true
		case StreamEventError:
			t.Fatalf("stream error: %v", ev.Err)
		}
	}
	if !done || content.String() != "hello" {
		t.Fatalf("done=%v content=%q", done, content.String())
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("calls=%d, want 2", got)
	}
}

func TestCompleteStream_StallWatchdog(t *testing.T) {
	prevStall := streamStallTimeout
	streamStallTimeout = 150 * time.Millisecond
	t.Cleanup(func() { streamStallTimeout = prevStall })

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
		fl.Flush()
		// Hang without closing: simulates a dead tunnel with TCP still up.
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)

	c := NewOpenAIClient(config.LLMConfig{Provider: "vllm", APIBase: srv.URL, Model: "m", TimeoutS: 1})
	ch, err := c.streamOnce(context.Background(), srv.URL+"/v1/chat/completions", CompleteRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, 128)
	if err != nil {
		t.Fatalf("streamOnce: %v", err)
	}
	var gotStall bool
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				if !gotStall {
					t.Fatal("channel closed without stall error")
				}
				return
			}
			if ev.Kind == StreamEventError && ev.Err != nil && strings.Contains(ev.Err.Error(), "stream stalled") {
				gotStall = true
			}
		case <-deadline:
			t.Fatal("watchdog did not fire within 5s")
		}
	}
}
