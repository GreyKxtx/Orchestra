package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// anthropicSSEDone is a minimal well-formed stream: one text block, then stop.
const anthropicSSEDone = `event: message_start
data: {"type":"message_start","message":{"usage":{"input_tokens":5,"output_tokens":0,"cache_read_input_tokens":100,"cache_creation_input_tokens":20}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}

event: message_delta
data: {"type":"message_delta","usage":{"input_tokens":5,"output_tokens":2,"cache_read_input_tokens":100,"cache_creation_input_tokens":20}}

event: message_stop
data: {"type":"message_stop"}

`

// TestAnthropicWire_CacheBreakpointsAndRoles inspects the actual JSON that
// leaves the client — the unit tests only cover the helpers in isolation.
func TestAnthropicWire_CacheBreakpointsAndRoles(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, anthropicSSEDone)
	}))
	defer srv.Close()

	c := NewAnthropicClient(LLMConfig{APIBase: srv.URL, APIKey: "k", Model: "claude-sonnet-4-5", MaxTokens: 1024})
	resp, err := c.Complete(context.Background(), CompleteRequest{
		Messages: []Message{
			{Role: RoleSystem, Content: "system prompt"},
			{Role: RoleUser, Content: "goal"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "t1", Type: "function",
				Function: ToolCallFunc{Name: "read", Arguments: ToolArguments(json.RawMessage(`{"path":"a.go"}`))}}}},
			{Role: RoleTool, ToolCallID: "t1", Content: "file body"},
			{Role: RoleUser, Content: "<working_state>volatile</working_state>"},
		},
		Tools: []ToolDef{
			{Type: "function", Function: ToolFunctionDef{Name: "read", Description: "read a file"}},
			{Type: "function", Function: ToolFunctionDef{Name: "edit", Description: "edit a file"}},
		},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	var sent struct {
		System []struct {
			CacheControl *struct{ Type string } `json:"cache_control"`
		} `json:"system"`
		Tools []struct {
			CacheControl *struct{ Type string } `json:"cache_control"`
		} `json:"tools"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("unmarshal request: %v\n%s", err, body)
	}

	// 1. System block is cached.
	if len(sent.System) != 1 || sent.System[0].CacheControl == nil {
		t.Errorf("system block carries no cache_control: %s", body)
	}
	// 2. The last tool schema is cached, earlier ones are not.
	if len(sent.Tools) != 2 || sent.Tools[1].CacheControl == nil || sent.Tools[0].CacheControl != nil {
		t.Errorf("tool cache breakpoint misplaced: %s", body)
	}
	// 3. Roles alternate — the volatile user block was folded into the
	//    tool_result message rather than sent as a second user message.
	for i := 1; i < len(sent.Messages); i++ {
		if sent.Messages[i].Role == sent.Messages[i-1].Role {
			t.Fatalf("consecutive %q messages — Anthropic rejects this: %s", sent.Messages[i].Role, body)
		}
	}
	// 4. The rolling breakpoint sits on the last stable message, not the volatile tail.
	if n := len(sent.Messages); n >= 2 {
		if !strings.Contains(string(sent.Messages[n-2].Content), `"cache_control"`) {
			t.Errorf("no rolling cache breakpoint before the volatile tail: %s", body)
		}
		if strings.Contains(string(sent.Messages[n-1].Content), `"cache_control"`) {
			t.Errorf("breakpoint landed on the volatile tail: %s", body)
		}
	}
	// 5. Cache counters survive the stream and reach TokenUsage.
	if resp.Usage == nil || resp.Usage.CachedPromptTokens != 100 || resp.Usage.CacheWriteTokens != 20 {
		t.Errorf("cache usage lost: %+v", resp.Usage)
	}
	if resp.Usage.PromptTokens != 125 {
		t.Errorf("prompt tokens = %d, want 125 (5 input + 100 read + 20 write)", resp.Usage.PromptTokens)
	}
}
