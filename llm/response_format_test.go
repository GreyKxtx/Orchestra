package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBuildChatBody_OmitsJSONSchemaWhenDisabled(t *testing.T) {
	falseVal := false
	c := NewOpenAIClient(LLMConfig{
		Provider:           "openai",
		APIBase:            "http://example.invalid",
		Model:              "m",
		SupportsJSONSchema: &falseVal,
	})
	body, err := c.buildChatBody(CompleteRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		ResponseFormat: &ResponseFormat{
			Type:       "json_schema",
			Schema:     []byte(`{"type":"object"}`),
			SchemaName: "agent_step",
		},
	}, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatal(err)
	}
	if _, ok := req["response_format"]; ok {
		t.Fatalf("expected response_format omitted, got %#v", req["response_format"])
	}
}

func TestBuildChatBody_ResponseGrammar(t *testing.T) {
	c := NewOpenAIClient(LLMConfig{Provider: "openai", APIBase: "http://example.invalid", Model: "m"})
	schema := []byte(`{"type":"object","properties":{}}`)
	body, err := c.buildChatBody(CompleteRequest{
		Messages:        []Message{{Role: RoleUser, Content: "hi"}},
		ResponseGrammar: schema,
	}, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatal(err)
	}
	rf, ok := req["response_format"].(map[string]any)
	if !ok || rf["type"] != "json_schema" {
		t.Fatalf("response_format = %#v", req["response_format"])
	}
}

func TestComplete_AutoDisablesJSONSchema(t *testing.T) {
	var calls int
	var lastBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		b, _ := io.ReadAll(r.Body)
		lastBody = b
		var req map[string]any
		_ = json.Unmarshal(b, &req)
		if _, has := req["response_format"]; has {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"response_format json_schema is not supported"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		writeAssistantSSE(w, "ok")
	}))
	t.Cleanup(srv.Close)

	c := NewOpenAIClient(LLMConfig{
		Provider: "openai",
		APIBase:  srv.URL,
		Model:    "m",
		TimeoutS: 5,
	})
	resp, err := c.Complete(context.Background(), CompleteRequest{
		Messages:       []Message{{Role: RoleUser, Content: "hi"}},
		ResponseFormat: GrammarFromJSONSchema([]byte(`{"type":"object"}`), "agent_step"),
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Message.Content != "ok" {
		t.Fatalf("content=%q", resp.Message.Content)
	}
	if calls < 2 {
		t.Fatalf("expected retry after disable, calls=%d", calls)
	}
	var req map[string]any
	_ = json.Unmarshal(lastBody, &req)
	if _, has := req["response_format"]; has {
		t.Fatalf("second request still had response_format: %#v", req["response_format"])
	}
}
