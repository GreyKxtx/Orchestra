package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every agent call is a stream — Complete is CompleteStream plus a drain — and
// none of them used to be logged. An eval's llm_log.jsonl carried tool calls
// and results and not one word the model said, so the question "why did it
// rewrite the file after a correct edit?" (edit_in_large_file, 2026-09-18) had
// no answer in the log. The stream has to log what the non-streaming path has
// logged all along: the request, and the answer as assembled.
func TestCompleteStream_LogsTheRequestAndTheAssembledAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"I will rewrite \"},\"finish_reason\":null}]}\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"the whole file.\"},\"finish_reason\":null}]}\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"write\",\"arguments\":\"{\\\"path\\\":\\\"limits.go\\\"}\"}}]},\"finish_reason\":null}]}\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":40,\"completion_tokens\":12,\"total_tokens\":52}}\n")
		fmt.Fprint(w, "data: [DONE]\n")
	}))
	t.Cleanup(srv.Close)

	root := t.TempDir()
	c := NewOpenAIClient(LLMConfig{Provider: "openai", APIBase: srv.URL, APIKey: "test", Model: "test-model", TimeoutS: 5})
	c.SetLogger(NewLogger(root))

	if _, err := c.Complete(context.Background(), CompleteRequest{
		Messages: []Message{{Role: RoleUser, Content: "change MaxRetries to 10"}},
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(root, ".orchestra", "llm_log.jsonl"))
	if err != nil {
		t.Fatalf("no llm_log.jsonl was written: %v", err)
	}
	var request, response *LLMLogEntry
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var e LLMLogEntry
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		switch e.Event {
		case "llm_request":
			request = &e
		case "llm_response":
			response = &e
		}
	}
	if request == nil {
		t.Fatalf("the stream logged no llm_request:\n%s", raw)
	}
	if !strings.Contains(request.RequestPreview, "change MaxRetries to 10") || request.MessagesCount != 1 {
		t.Errorf("the request entry does not describe the request sent: %+v", request)
	}
	if response == nil {
		t.Fatalf("the stream logged no llm_response:\n%s", raw)
	}
	for _, want := range []string{"I will rewrite the whole file.", `"write"`, `limits.go`, `"completion_tokens":12`} {
		if !strings.Contains(response.ResponsePreview, want) {
			t.Errorf("the response entry lacks %q — the model's words are what the log is for:\n%s", want, response.ResponsePreview)
		}
	}
	if response.DurationMS < 0 {
		t.Errorf("duration must be recorded, got %d", response.DurationMS)
	}
}

// A refused request is logged too, with the server's status and body.
func TestCompleteStream_LogsARefusedRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":{"message":"unknown field chat_template_kwargs"}}`)
	}))
	t.Cleanup(srv.Close)

	root := t.TempDir()
	c := NewOpenAIClient(LLMConfig{Provider: "openai", APIBase: srv.URL, APIKey: "test", Model: "test-model", TimeoutS: 5})
	c.SetLogger(NewLogger(root))

	if _, err := c.Complete(context.Background(), CompleteRequest{Messages: []Message{{Role: RoleUser, Content: "ping"}}}); err == nil {
		t.Fatal("a 400 must surface as an error")
	}
	raw, err := os.ReadFile(filepath.Join(root, ".orchestra", "llm_log.jsonl"))
	if err != nil {
		t.Fatalf("no llm_log.jsonl was written: %v", err)
	}
	if !strings.Contains(string(raw), `"event": "llm_error"`) && !strings.Contains(string(raw), `"event":"llm_error"`) {
		t.Fatalf("the refusal was not logged as llm_error:\n%s", raw)
	}
	if !strings.Contains(string(raw), "chat_template_kwargs") {
		t.Fatalf("the server's reason is missing from the log:\n%s", raw)
	}
}
