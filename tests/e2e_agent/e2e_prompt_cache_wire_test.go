package e2e_agent

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/orchestra/orchestra/internal/agent"
	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// sseToolCall / sseFinal are OpenAI-compatible streamed replies.
func sseToolCall(id, name, args string) string {
	delta, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{
			"tool_calls": []any{map[string]any{
				"index": 0, "id": id, "type": "function",
				"function": map[string]any{"name": name, "arguments": args},
			}},
		}}},
	})
	stop, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls"}},
	})
	return fmt.Sprintf("data: %s\n\ndata: %s\n\ndata: [DONE]\n\n", delta, stop)
}

func sseFinal(content string) string {
	delta, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": content}}},
	})
	stop, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
		"usage": map[string]any{
			"prompt_tokens": 100, "completion_tokens": 5, "total_tokens": 105,
			"prompt_tokens_details": map[string]any{"cached_tokens": 80},
		},
	})
	return fmt.Sprintf("data: %s\n\ndata: %s\n\ndata: [DONE]\n\n", delta, stop)
}

// TestAgent_E2E_PromptPrefixStableOnTheWire drives a real OpenAIClient over
// HTTP and compares the *serialized request bodies* across steps.
//
// The unit test asserts the agent builds a stable prefix; this asserts nothing
// between the agent and the socket reorders or rewrites it. A prefix that
// differs by a single byte costs the whole prompt cache.
func TestAgent_E2E_PromptPrefixStableOnTheWire(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var bodies []string
	step := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		n := step
		step++
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		if n < 3 {
			_, _ = io.WriteString(w, sseToolCall(fmt.Sprintf("call_%d", n), "ls", `{"path":"."}`))
			return
		}
		_, _ = io.WriteString(w, sseFinal(`{"patches":[]}`))
	}))
	defer srv.Close()

	runner, err := tools.NewRunner(dir, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}

	client := llm.NewClient(llm.LLMConfig{APIBase: srv.URL, APIKey: "k", Model: "claude-sonnet-4-5", MaxTokens: 512, TimeoutS: 30})
	var lastUsage *llm.TokenUsage
	ag, err := agent.New(client, v, runner, agent.Options{
		MaxSteps:     8,
		PromptFamily: "anthropic",
		OnEvent: func(ev agent.AgentEvent) {
			if ev.Stream.Kind != llm.StreamEventStepUsage {
				return
			}
			// Two emitters share this event kind: the provider's real usage and
			// the TUI context estimate (tagged source=estimate). Only the real
			// one carries cache counters.
			var probe struct {
				Source string `json:"source"`
			}
			_ = json.Unmarshal([]byte(ev.Stream.Content), &probe)
			if probe.Source == "estimate" {
				return
			}
			var u llm.TokenUsage
			if json.Unmarshal([]byte(ev.Stream.Content), &u) == nil && u.PromptTokens > 0 {
				lastUsage = &u
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ag.Run(t.Context(), nil, "list the workspace"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) < 3 {
		t.Fatalf("expected at least 3 requests, got %d", len(bodies))
	}

	type wire struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Tools []json.RawMessage `json:"tools"`
	}
	prefixOf := func(raw string) (string, string) {
		var w wire
		if err := json.Unmarshal([]byte(raw), &w); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		if len(w.Messages) < 2 {
			t.Fatalf("request has %d messages", len(w.Messages))
		}
		toolsJSON, _ := json.Marshal(w.Tools)
		return w.Messages[0].Content + "\x00" + w.Messages[1].Content, string(toolsJSON)
	}

	basePrefix, baseTools := prefixOf(bodies[1])
	for i := 2; i < len(bodies); i++ {
		gotPrefix, gotTools := prefixOf(bodies[i])
		if gotPrefix != basePrefix {
			t.Errorf("request %d: cacheable prefix changed since request 2 — every step would miss the cache", i+1)
		}
		if gotTools != baseTools {
			t.Errorf("request %d: tool schemas changed between steps", i+1)
		}
	}

	// The OpenAI-compatible cache counter must survive the stream parser and
	// reach the per-step usage event the TUI reads.
	if lastUsage == nil || lastUsage.CachedPromptTokens != 80 {
		t.Errorf("prompt_tokens_details.cached_tokens lost on the way to TokenUsage: %+v", lastUsage)
	}

	// The volatile block must be present and last, not in the prefix.
	var last wire
	if err := json.Unmarshal([]byte(bodies[len(bodies)-1]), &last); err != nil {
		t.Fatal(err)
	}
	tail := last.Messages[len(last.Messages)-1]
	if tail.Role != "user" || !strings.Contains(tail.Content, "<working_state>") {
		t.Errorf("volatile block is not the last message on the wire: role=%s content=%.80q", tail.Role, tail.Content)
	}
}
