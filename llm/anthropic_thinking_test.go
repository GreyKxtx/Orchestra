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

// A thinking-plus-tool_use answer as the API streams it: a thinking block
// with its signature, a redacted one, then the tool call.
const anthropicThinkingToolSSE = `event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"read the file "}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"first"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"SIG-1"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"redacted_thinking","data":"REDACTED-1"}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: content_block_start
data: {"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_1","name":"read","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"a.go\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":2}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"}}

event: message_stop
data: {"type":"message_stop"}

`

// LLM-6: the thinking blocks and their signatures were streamed to the UI and
// dropped; the answer kept no trace of them to send back.
func TestParseAnthropicSSEStream_KeepsThinkingBlocks(t *testing.T) {
	var reasoning string
	var done *CompleteResponse
	for ev := range ParseAnthropicSSEStream(context.Background(), strings.NewReader(anthropicThinkingToolSSE)) {
		switch ev.Kind {
		case StreamEventReasoningDelta:
			reasoning += ev.Content
		case StreamEventDone:
			done = ev.Response
		case StreamEventError:
			t.Fatal(ev.Err)
		}
	}
	if reasoning != "read the file first" {
		t.Fatalf("reasoning shown: %q", reasoning)
	}
	want := []ThinkingBlock{{Text: "read the file first", Signature: "SIG-1"}, {Redacted: "REDACTED-1"}}
	if done == nil || len(done.Message.Thinking) != 2 || done.Message.Thinking[0] != want[0] || done.Message.Thinking[1] != want[1] {
		t.Fatalf("thinking kept: %+v", done)
	}
	if done.Message.Content != "" || len(done.Message.ToolCalls) != 1 {
		t.Fatalf("thinking must not leak into the content: %+v", done.Message)
	}
}

// The blocks go back first, unmodified, in order — a hidden thinking's empty
// text still sent as "thinking":"" — and a cache breakpoint never lands on
// one.
func TestConvertToAnthropic_ReplaysThinkingBeforeToolUse(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: "read a.go"},
		{Role: RoleAssistant, Thinking: []ThinkingBlock{{Signature: "SIG-HIDDEN"}, {Redacted: "R"}},
			ToolCalls: []ToolCall{{ID: "toolu_1", Type: "function", Function: ToolCallFunc{Name: "read", Arguments: ToolArguments(`{"path":"a.go"}`)}}}},
		{Role: RoleTool, ToolCallID: "toolu_1", Content: "package a"},
	}
	_, out := convertToAnthropic(msgs)
	raw, _ := json.Marshal(out[1].Content)
	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 3 || blocks[0]["type"] != "thinking" || blocks[1]["type"] != "redacted_thinking" || blocks[2]["type"] != "tool_use" {
		t.Fatalf("assistant blocks: %s", raw)
	}
	if th, present := blocks[0]["thinking"]; !present || th != "" || blocks[0]["signature"] != "SIG-HIDDEN" {
		t.Fatalf("hidden thinking must go back with its empty text and signature: %s", raw)
	}
	if blocks[1]["data"] != "R" {
		t.Fatalf("redacted thinking goes back with its data: %s", raw)
	}

	// An assistant turn that is only thinking: the breakpoint skips it.
	only := []anthropicMessage{{Role: "assistant", Content: thinkingToAnthropic([]ThinkingBlock{{Signature: "S"}})}, {Role: "user", Content: "x"}}
	markPrefixCacheBreakpoint(only)
	for _, b := range only[0].Content.([]anthropicBlock) {
		if b.CacheControl != nil {
			t.Fatal("cache_control on a thinking block is a 400")
		}
	}
}

// Thinking blocks are Anthropic's: an OpenAI-compatible request never
// carries them, while the history itself keeps them (sessions, checkpoints).
func TestThinking_PersistedButNotSentToOpenAI(t *testing.T) {
	m := Message{Role: RoleAssistant, Thinking: []ThinkingBlock{{Text: "t", Signature: "S"}},
		ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunc{Name: "read", Arguments: ToolArguments(`{}`)}}}}
	raw, _ := json.Marshal(m)
	var back Message
	if err := json.Unmarshal(raw, &back); err != nil || len(back.Thinking) != 1 || back.Thinking[0].Signature != "S" {
		t.Fatalf("round trip: %s → %+v %v", raw, back, err)
	}

	c := NewOpenAIClient(LLMConfig{Provider: "vllm", APIBase: "http://127.0.0.1:1", Model: "m"})
	body, err := c.buildChatBody(CompleteRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}, m}}, 64, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "thinking") || strings.Contains(string(body), `"S"`) {
		t.Fatalf("thinking reached an OpenAI-compatible request: %s", body)
	}
}

func TestAnthropicAdaptiveThinking(t *testing.T) {
	for model, want := range map[string]bool{
		"claude-3-7-sonnet-20250219":                false,
		"claude-sonnet-4-20250514":                  false,
		"claude-opus-4-1-20250805":                  false,
		"claude-opus-4-5-20251101":                  false,
		"claude-haiku-4-5-20251001":                 false,
		"anthropic.claude-sonnet-4-5-20250929-v1:0": false,
		"claude-sonnet-4-5@20250929":                false,
		"claude-sonnet-4-6":                         true,
		"claude-opus-4-7":                           true,
		"claude-opus-5-5":                           true,
		"claude-sonnet-5":                           true,
		"claude-fable-5-1":                          true,
		"claude-mythos-preview":                     true,
		// Other vendors behind Anthropic-compatible APIs keep the old request.
		"kimi-k2-0905-preview": false,
		"deepseek-chat":        false,
		"MiniMax-M2":           false,
	} {
		if got := anthropicAdaptiveThinking(model); got != want {
			t.Errorf("%s: adaptive=%v, want %v", model, got, want)
		}
	}
}

func captureAnthropic(t *testing.T, cfg LLMConfig, req CompleteRequest) map[string]any {
	t.Helper()
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, anthropicSSEDone)
	}))
	defer srv.Close()
	cfg.APIBase, cfg.APIKey = srv.URL, "k"
	if _, err := NewAnthropicClient(cfg).Complete(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	var sent map[string]any
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatal(err)
	}
	return sent
}

// LLM-6: every model got {type:"enabled", budget_tokens}. Claude 4.7 and
// later answer that with a 400, and 4.6 deprecates it: they take
// {type:"adaptive"} and an effort level. Their thinking is shown only when
// asked for, and their default max_tokens leaves room to think.
func TestAnthropic_AdaptiveThinkingOnNewModels(t *testing.T) {
	hi := CompleteRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}}
	sent := captureAnthropic(t, LLMConfig{Model: "claude-opus-5-5", Reasoning: &ReasoningConfig{Effort: "high"}}, hi)
	th, _ := sent["thinking"].(map[string]any)
	if th["type"] != "adaptive" || th["display"] != "summarized" || th["budget_tokens"] != nil {
		t.Fatalf("thinking = %v, want adaptive/summarized with no budget", sent["thinking"])
	}
	if oc, _ := sent["output_config"].(map[string]any); oc["effort"] != "high" {
		t.Fatalf("output_config = %v, want effort high", sent["output_config"])
	}
	if mt, _ := sent["max_tokens"].(float64); mt < 16000 {
		t.Fatalf("max_tokens = %v: thinking and answer share it", mt)
	}

	// A model that thinks by default needs no reasoning config to get room.
	plain := captureAnthropic(t, LLMConfig{Model: "claude-sonnet-5"}, hi)
	if plain["thinking"] != nil || plain["output_config"] != nil {
		t.Fatalf("no reasoning asked, none sent: %v", plain)
	}

	// 4.5 and earlier keep the budget.
	old := captureAnthropic(t, LLMConfig{Model: "claude-sonnet-4-5", Reasoning: &ReasoningConfig{Effort: "high"}}, hi)
	if th, _ := old["thinking"].(map[string]any); th["type"] != "enabled" || th["budget_tokens"] != float64(16384) {
		t.Fatalf("thinking on 4.5 = %v, want enabled/16384", old["thinking"])
	}
	if old["output_config"] != nil {
		t.Fatalf("no effort sent to 4.5: %v", old["output_config"])
	}
}

// LLM-7: a user message's Parts — screenshots, and with --image the query's
// own text — were dropped: only Content was read.
func TestConvertToAnthropic_Parts(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Parts: []ContentPart{
			{Kind: PartText, Text: "what is on this screen?"},
			{Kind: PartImage, ImageData: []byte("PNG"), ImageMIME: "image/png"},
			{Kind: PartImage, ImageURL: "data:image/jpeg;base64,SlBH"},
			{Kind: PartImage, ImageURL: "https://example.com/a.png"},
		}},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "t1", Type: "function", Function: ToolCallFunc{Name: "browser.screenshot", Arguments: ToolArguments(`{}`)}}}},
		{Role: RoleTool, ToolCallID: "t1", Parts: []ContentPart{{Kind: PartText, Text: "shot"}, {Kind: PartImage, ImageData: []byte("IMG"), ImageMIME: "image/png"}}},
	}
	_, out := convertToAnthropic(msgs)
	raw, _ := json.Marshal(out)
	var wire []struct {
		Content []map[string]any `json:"content"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	user := wire[0].Content
	if len(user) != 4 || user[0]["text"] != "what is on this screen?" {
		t.Fatalf("user blocks: %s", raw)
	}
	src := func(b map[string]any) map[string]any { s, _ := b["source"].(map[string]any); return s }
	if s := src(user[1]); user[1]["type"] != "image" || s["type"] != "base64" || s["media_type"] != "image/png" || s["data"] != "UE5H" {
		t.Fatalf("image bytes: %v", user[1])
	}
	if s := src(user[2]); s["media_type"] != "image/jpeg" || s["data"] != "SlBH" {
		t.Fatalf("data URI: %v", user[2])
	}
	if s := src(user[3]); s["type"] != "url" || s["url"] != "https://example.com/a.png" {
		t.Fatalf("remote image: %v", user[3])
	}
	result := wire[2].Content[0]
	parts, _ := result["content"].([]any)
	if result["type"] != "tool_result" || len(parts) != 2 {
		t.Fatalf("a tool result's image is sent with it: %v", result)
	}
}
