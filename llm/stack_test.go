package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ARCH-8: the request log was the OpenAI-compatible client's alone. With
// provider: anthropic, BuildClient attached no logger, every caller that
// asked for one through AsOpenAIClient got nil, and llm_log.jsonl stayed
// empty for the whole run.
func TestAnthropic_WritesTheRequestLog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, anthropicSSEDone)
	}))
	t.Cleanup(srv.Close)
	root := t.TempDir()
	c := BuildClient(LLMConfig{Provider: "anthropic", APIBase: srv.URL, APIKey: "k", Model: "claude-sonnet-4-5"}, ProviderRegistry{}, NewLogger(root))
	if LoggerOf(c) == nil {
		t.Fatal("the Anthropic client carries no logger")
	}
	if _, err := c.Complete(context.Background(), CompleteRequest{Messages: []Message{{Role: RoleUser, Content: "ping the log"}}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".orchestra", "llm_log.jsonl"))
	if err != nil {
		t.Fatalf("no llm_log.jsonl: %v", err)
	}
	if !strings.Contains(string(data), "ping the log") || !strings.Contains(string(data), `\"ok\"`) {
		t.Fatalf("the log misses the request or the answer:\n%s", data)
	}
}

// The stack helpers see through the decorators to the provider client.
func TestStackHelpers_SeeThroughDecorators(t *testing.T) {
	logger := NewLogger(t.TempDir())
	primary := NewAnthropicClient(LLMConfig{APIKey: "k", Model: "claude-sonnet-4-5"})
	primary.SetLogger(logger)
	openai := NewOpenAIClient(LLMConfig{Provider: "vllm", APIBase: "http://127.0.0.1:1", Model: "m"})
	openai.SetContextTokens(4096)
	stack := &RouterClient{Main: NewFallbackClient(primary, "anthropic", openai, "standby")}
	if LoggerOf(stack) != logger {
		t.Fatal("LoggerOf does not reach the Anthropic client under the router and the fallback")
	}
	if got := ContextTokensOf(stack); got != 4096 {
		t.Fatalf("ContextTokensOf = %d: the fallback reports its providers' window", got)
	}
	if _, ok, _ := DiscoverLimits(context.Background(), primary); ok {
		t.Fatal("an Anthropic client has no limits endpoint to ask")
	}
}
