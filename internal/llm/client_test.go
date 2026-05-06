package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
)

func TestOpenAIClient_BuildsToolsPayload_AndParsesToolCalls(t *testing.T) {
	var gotBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("expected path /v1/chat/completions, got %s", r.URL.Path)
		}
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		gotBody = b

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "choices": [
    {
      "message": {
        "role": "assistant",
        "content": "",
        "tool_calls": [
          {
            "id": "call_1",
            "type": "function",
            "function": {
              "name": "fs.read",
              "arguments": "{\"path\":\"a.txt\",\"max_bytes\":123}"
            }
          }
        ]
      }
    }
  ]
}`))
	}))
	t.Cleanup(srv.Close)

	c := NewOpenAIClient(config.LLMConfig{
		APIBase:     srv.URL, // client will append /v1 if missing
		APIKey:      "test",
		Model:       "test-model",
		MaxTokens:   1234,
		Temperature: 0.0,
		TimeoutS:    5,
	})

	resp, err := c.Complete(context.Background(), CompleteRequest{
		Messages: []Message{
			{Role: RoleSystem, Content: "system"},
			{Role: RoleUser, Content: "user"},
		},
		Tools: []ToolDef{
			{
				Type: "function",
				Function: ToolFunctionDef{
					Name:       "fs.read",
					Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	if resp == nil {
		t.Fatalf("expected response, got nil")
	}
	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.Message.ToolCalls))
	}
	tc := resp.Message.ToolCalls[0]
	if tc.Type != "function" {
		t.Fatalf("expected type=function, got %q", tc.Type)
	}
	if tc.Function.Name != "fs.read" {
		t.Fatalf("expected tool name fs.read, got %q", tc.Function.Name)
	}
	if string(tc.Function.Arguments.Raw()) != `{"path":"a.txt","max_bytes":123}` {
		t.Fatalf("unexpected tool arguments: %s", string(tc.Function.Arguments.Raw()))
	}

	// Validate request payload contains tools + tool_choice=auto.
	var req map[string]any
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatalf("unmarshal request: %v\nbody=%s", err, string(gotBody))
	}
	if req["model"] != "test-model" {
		t.Fatalf("expected model=test-model, got %#v", req["model"])
	}
	if _, ok := req["tools"]; !ok {
		t.Fatalf("expected tools in request, got: %s", string(gotBody))
	}
	if req["tool_choice"] != "auto" {
		t.Fatalf("expected tool_choice=auto, got %#v", req["tool_choice"])
	}
}
