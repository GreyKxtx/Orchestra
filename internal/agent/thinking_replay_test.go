package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/orchestra/orchestra/internal/tools"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol/schema"
)

// fakeAnthropic answers the first step with thinking and a read, and holds
// the second to the API's rule: the assistant turn with the tool_use must
// carry its thinking back, signature intact, or the request is a 400.
type fakeAnthropic struct {
	mu    sync.Mutex
	steps int
	saw   []string
	// parallel makes the first answer two reads: a parallel batch.
	parallel bool
}

func (f *fakeAnthropic) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.steps++
	n := f.steps
	f.saw = append(f.saw, string(body))
	f.mu.Unlock()
	if n > 1 {
		var req struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(body, &req)
		ok := false
		for _, m := range req.Messages {
			if m.Role == "assistant" && strings.Contains(string(m.Content), `"tool_use"`) {
				var blocks []map[string]any
				_ = json.Unmarshal(m.Content, &blocks)
				ok = len(blocks) > 0 && blocks[0]["type"] == "thinking" && blocks[0]["signature"] == "SIG-1"
			}
		}
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"messages.1.content.0.type: Expected `+"`thinking`"+` or `+"`redacted_thinking`"+`, but found `+"`tool_use`"+`."}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: content_block_delta\n"+
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"a.go declares package a."}}`+"\n\n"+
			"event: message_delta\n"+`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}`+"\n\n"+
			"event: message_stop\n"+`data: {"type":"message_stop"}`+"\n\n")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, "event: content_block_start\n"+
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`+"\n\n"+
		"event: content_block_delta\n"+
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"SIG-1"}}`+"\n\n"+
		"event: content_block_start\n"+
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"read","input":{}}}`+"\n\n"+
		"event: content_block_delta\n"+
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"a.go\"}"}}`+"\n\n")
	if f.parallel {
		_, _ = io.WriteString(w, "event: content_block_start\n"+
			`data: {"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_2","name":"read","input":{}}}`+"\n\n"+
			"event: content_block_delta\n"+
			`data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"b.go\"}"}}`+"\n\n")
	}
	_, _ = io.WriteString(w, "event: message_delta\n"+`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"}}`+"\n\n"+
		"event: message_stop\n"+`data: {"type":"message_stop"}`+"\n\n")
}

// LLM-6 acceptance: on a model that thinks — every Claude 5 model does by
// default — the step after a tool call sends the thinking back beside the
// tool_use. It was dropped, and the second step of every such turn was a
// 400 from the API.
func TestAgent_ThinkingGoesBackWithTheToolCall(t *testing.T) {
	for _, parallel := range []bool{false, true} {
		runThinkingReplay(t, parallel)
	}
}

func runThinkingReplay(t *testing.T, parallel bool) {
	fake := &fakeAnthropic{parallel: parallel}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	root := t.TempDir()
	for _, f := range []string{"a.go", "b.go"} {
		if err := os.WriteFile(filepath.Join(root, f), []byte("package a\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	v, err := schema.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tools.NewRunner(root, tools.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	client := llm.NewAnthropicClient(llm.LLMConfig{APIBase: srv.URL, APIKey: "k", Model: "claude-sonnet-5"})
	ag, err := New(client, v, tr, Options{Mode: ModeAsk, MaxSteps: 4})
	if err != nil {
		t.Fatal(err)
	}
	history, _, err := ag.Run(context.Background(), nil, "what package is a.go?")
	if err != nil {
		t.Fatalf("parallel=%v Run: %v\nsecond request: %s", parallel, err, fake.saw[len(fake.saw)-1])
	}
	kept := false
	for _, m := range history {
		if len(m.ToolCalls) > 0 && len(m.Thinking) == 1 && m.Thinking[0].Signature == "SIG-1" {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("parallel=%v: the history keeps the thinking beside its tool call (sessions and checkpoints resume from it)", parallel)
	}
}
