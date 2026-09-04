package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func gatewayClient(t *testing.T, model, base string) *OpenAIClient {
	t.Helper()
	return NewOpenAIClient(LLMConfig{
		Provider: "openrouter",
		APIBase:  base,
		APIKey:   "k",
		Model:    model,
		TimeoutS: 5,
	})
}

func decodeBody(t *testing.T, body []byte) []any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("unmarshal body: %v\n%s", err, body)
	}
	msgs, ok := m["messages"].([]any)
	if !ok {
		t.Fatalf("no messages in body: %s", body)
	}
	return msgs
}

func hasCacheControl(msg any) bool {
	m, ok := msg.(map[string]any)
	if !ok {
		return false
	}
	parts, ok := m["content"].([]any)
	if !ok {
		return false
	}
	for _, p := range parts {
		pm, ok := p.(map[string]any)
		if ok && pm["cache_control"] != nil {
			return true
		}
	}
	return false
}

func agentShapedMessages() []Message {
	// The layout the agent actually sends: a stable system block, a stable
	// first user message, append-only history, and a volatile tail rebuilt
	// on every step.
	return []Message{
		{Role: RoleSystem, Content: "system prompt + project memory"},
		{Role: RoleUser, Content: "the task"},
		{Role: RoleAssistant, Content: "looking"},
		{Role: RoleUser, Content: "<working_state>…</working_state>"},
	}
}

func TestChatBody_MarksCacheBreakpointsForAnthropicViaGateway(t *testing.T) {
	c := gatewayClient(t, "anthropic/claude-sonnet-5", "http://example/v1")

	body, err := c.buildChatBody(CompleteRequest{Messages: agentShapedMessages()}, 128, false)
	if err != nil {
		t.Fatal(err)
	}

	msgs := decodeBody(t, body)
	// A step re-sends the whole transcript. Without a breakpoint the gateway
	// bills all of it at full price on every step — one field turn spent
	// 983k prompt tokens over 15 calls.
	if !hasCacheControl(msgs[0]) {
		t.Errorf("system block must be a cache breakpoint: %s", body)
	}
	if !hasCacheControl(msgs[len(msgs)-2]) {
		t.Errorf("the last message before the volatile tail must be a breakpoint: %s", body)
	}
	// Marking the tail would be pointless: it is rebuilt every step, so it can
	// never be a cache hit, and it would burn one of Anthropic's four slots.
	if hasCacheControl(msgs[len(msgs)-1]) {
		t.Errorf("the volatile tail must not be marked: %s", body)
	}
}

func TestChatBody_LeavesNonAnthropicModelsUnmarked(t *testing.T) {
	c := gatewayClient(t, "qwen/qwen3-27b", "http://example/v1")

	body, err := c.buildChatBody(CompleteRequest{Messages: agentShapedMessages()}, 128, false)
	if err != nil {
		t.Fatal(err)
	}

	// Only Anthropic models read cache_control. Other providers cache by
	// prefix automatically, and a self-hosted OpenAI-compatible server may
	// reject the unknown field outright.
	for i, m := range decodeBody(t, body) {
		if hasCacheControl(m) {
			t.Fatalf("message %d carries cache_control for a non-Anthropic model: %s", i, body)
		}
	}
}

func TestChatBody_LeavesToolCallOnlyMessagesAlone(t *testing.T) {
	c := gatewayClient(t, "anthropic/claude-sonnet-5", "http://example/v1")
	msgs := []Message{
		{Role: RoleSystem, Content: "system"},
		{Role: RoleUser, Content: "task"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "1", Type: "function"}}},
		{Role: RoleUser, Content: "volatile"},
	}

	body, err := c.buildChatBody(CompleteRequest{Messages: msgs}, 128, false)
	if err != nil {
		t.Fatal(err)
	}

	// An assistant message that is only tool_calls has no text block to mark;
	// rewriting its content into an empty array would drop the tool calls.
	decoded := decodeBody(t, body)
	assistant, _ := decoded[2].(map[string]any)
	if _, isArray := assistant["content"].([]any); isArray {
		t.Fatalf("tool-call-only message must keep its shape: %s", body)
	}
	if assistant["tool_calls"] == nil {
		t.Fatalf("tool_calls must survive: %s", body)
	}
}

func TestClient_StopsMarkingAfterTheGatewayRejectsIt(t *testing.T) {
	var bodies [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, b)
		if len(bodies) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":{"message":"unsupported parameter: cache_control"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n")
		fmt.Fprint(w, "data: [DONE]\n")
	}))
	t.Cleanup(srv.Close)
	c := gatewayClient(t, "anthropic/claude-sonnet-5", srv.URL)

	resp, err := c.Complete(context.Background(), CompleteRequest{Messages: agentShapedMessages()})

	// A gateway that does not understand the markers must cost one retry, not
	// the whole run: every subsequent step would 400 identically.
	if err != nil {
		t.Fatalf("client must recover by dropping the markers: %v", err)
	}
	if resp.Message.Content != "ok" {
		t.Fatalf("content = %q", resp.Message.Content)
	}
	if len(bodies) != 2 {
		t.Fatalf("expected one rejected attempt and one retry, got %d requests", len(bodies))
	}
	if !strings.Contains(string(bodies[0]), "cache_control") {
		t.Errorf("first attempt should have carried the markers: %s", bodies[0])
	}
	if strings.Contains(string(bodies[1]), "cache_control") {
		t.Errorf("retry must drop the markers: %s", bodies[1])
	}
}
