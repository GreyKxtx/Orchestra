package llm

import (
	"encoding/json"
	"testing"
)

func TestConvertToAnthropic_MergesTrailingUserBlock(t *testing.T) {
	// The agent appends its volatile context as a separate user message after
	// the tool results; Anthropic requires alternating roles, so it must be
	// folded into the preceding user message rather than sent as a second one.
	msgs := []Message{
		{Role: RoleSystem, Content: "sys"},
		{Role: RoleUser, Content: "goal"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "t1", Type: "function",
			Function: ToolCallFunc{Name: "ls", Arguments: ToolArguments(json.RawMessage(`{}`))}}}},
		{Role: RoleTool, ToolCallID: "t1", Content: "result"},
		{Role: RoleUser, Content: "<working_state>…</working_state>"},
	}
	system, out := convertToAnthropic(msgs)
	if system != "sys" {
		t.Fatalf("system=%q", system)
	}
	for i := 1; i < len(out); i++ {
		if out[i].Role == out[i-1].Role {
			t.Fatalf("consecutive %q messages at %d — Anthropic rejects this", out[i].Role, i)
		}
	}
	last := out[len(out)-1]
	blocks, ok := last.Content.([]anthropicBlock)
	if !ok || len(blocks) != 2 {
		t.Fatalf("last message content=%#v want 2 blocks (tool_result + text)", last.Content)
	}
	if blocks[0].Type != "tool_result" || blocks[1].Type != "text" {
		t.Fatalf("block types=%q,%q", blocks[0].Type, blocks[1].Type)
	}
}

func TestMarkPrefixCacheBreakpoint(t *testing.T) {
	_, msgs := convertToAnthropic([]Message{
		{Role: RoleUser, Content: "goal"},
		{Role: RoleAssistant, Content: "thinking"},
		{Role: RoleUser, Content: "volatile"},
	})
	markPrefixCacheBreakpoint(msgs)

	// Breakpoint goes on the last stable message, not on the volatile tail.
	stable := msgs[len(msgs)-2]
	blocks, ok := stable.Content.([]anthropicBlock)
	if !ok || len(blocks) == 0 || blocks[len(blocks)-1].CacheControl == nil {
		t.Fatalf("no cache breakpoint on the stable prefix: %#v", stable.Content)
	}
	if tail, ok := msgs[len(msgs)-1].Content.([]anthropicBlock); ok && len(tail) > 0 && tail[len(tail)-1].CacheControl != nil {
		t.Fatal("volatile tail must not carry the breakpoint")
	}
}

func TestMarkToolsCacheBreakpoint(t *testing.T) {
	tools := convertTools([]ToolDef{
		{Function: ToolFunctionDef{Name: "a"}},
		{Function: ToolFunctionDef{Name: "b"}},
	})
	markToolsCacheBreakpoint(tools)
	if tools[0].CacheControl != nil {
		t.Fatal("only the last tool carries the breakpoint")
	}
	if tools[len(tools)-1].CacheControl == nil {
		t.Fatal("tool schemas are identical every step and must be cached")
	}
}

func TestAnthropicStreamUsage_CountsCacheTokens(t *testing.T) {
	u := &anthropicStreamUsage{InputTokens: 10, OutputTokens: 5, CacheReadInputTokens: 900, CacheCreationInputTokens: 90}
	got := u.toTokenUsage()
	if got.PromptTokens != 1000 {
		t.Fatalf("prompt=%d want 1000 (input + cache read + cache write)", got.PromptTokens)
	}
	if got.CachedPromptTokens != 900 || got.CacheWriteTokens != 90 {
		t.Fatalf("cache counters=%d/%d", got.CachedPromptTokens, got.CacheWriteTokens)
	}
}
