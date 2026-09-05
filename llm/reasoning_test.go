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

func bodyOf(t *testing.T, c *OpenAIClient) map[string]any {
	t.Helper()
	raw, err := c.buildChatBody(CompleteRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}}, 4096, false)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestReasoning_OpenAIDirectSendsReasoningEffort(t *testing.T) {
	c := NewOpenAIClient(LLMConfig{
		APIBase:   "https://api.openai.com/v1",
		Model:     "gpt-5",
		Reasoning: &ReasoningConfig{Effort: "high"},
	})
	if got := bodyOf(t, c)["reasoning_effort"]; got != "high" {
		t.Errorf("reasoning_effort = %v, want high", got)
	}
}

func TestReasoning_OpenRouterSendsTheNestedObject(t *testing.T) {
	// OpenRouter does not accept reasoning_effort; it takes a reasoning
	// object, and that is also the only way to pass a token budget through it.
	c := NewOpenAIClient(LLMConfig{
		APIBase:   "https://openrouter.ai/api/v1",
		Model:     "anthropic/claude-sonnet-4.5",
		Reasoning: &ReasoningConfig{Effort: "medium", BudgetTokens: 8000},
	})
	body := bodyOf(t, c)
	if _, ok := body["reasoning_effort"]; ok {
		t.Error("reasoning_effort must not be sent to OpenRouter")
	}
	obj, ok := body["reasoning"].(map[string]any)
	if !ok {
		t.Fatalf("reasoning = %v, want an object", body["reasoning"])
	}
	if obj["effort"] != "medium" {
		t.Errorf("reasoning.effort = %v, want medium", obj["effort"])
	}
	if obj["max_tokens"] != float64(8000) {
		t.Errorf("reasoning.max_tokens = %v, want 8000", obj["max_tokens"])
	}
}

func TestReasoning_SkippedForAModelThatCannotReason(t *testing.T) {
	// gpt-4o has no reasoning control; sending one is a 400. The capability
	// snapshot is what makes this checkable instead of guessed from the name.
	c := NewOpenAIClient(LLMConfig{
		APIBase:   "https://api.openai.com/v1",
		Model:     "gpt-4o",
		Reasoning: &ReasoningConfig{Effort: "high"},
	})
	body := bodyOf(t, c)
	if _, ok := body["reasoning_effort"]; ok {
		t.Error("reasoning_effort sent to a model with no reasoning capability")
	}
	if _, ok := body["reasoning"]; ok {
		t.Error("reasoning object sent to a model with no reasoning capability")
	}
}

func TestReasoning_UnknownModelTrustsTheUser(t *testing.T) {
	// A local or brand-new model the snapshot does not list: the user asked
	// for reasoning explicitly, so send it rather than silently dropping it.
	c := NewOpenAIClient(LLMConfig{
		APIBase:   "https://api.openai.com/v1",
		Model:     "my-own-reasoning-finetune",
		Reasoning: &ReasoningConfig{Effort: "low"},
	})
	if got := bodyOf(t, c)["reasoning_effort"]; got != "low" {
		t.Errorf("reasoning_effort = %v, want low for an unlisted model", got)
	}
}

func TestReasoning_NotConfiguredChangesNothing(t *testing.T) {
	c := NewOpenAIClient(LLMConfig{APIBase: "https://api.openai.com/v1", Model: "gpt-5"})
	body := bodyOf(t, c)
	if _, ok := body["reasoning_effort"]; ok {
		t.Error("reasoning_effort appeared without configuration")
	}
}

func TestReasoningBudget_DerivedFromEffort(t *testing.T) {
	cases := map[string]int{"low": 2048, "medium": 8192, "high": 16384}
	for effort, want := range cases {
		if got := (&ReasoningConfig{Effort: effort}).budget(); got != want {
			t.Errorf("budget(%q) = %d, want %d", effort, got, want)
		}
	}
	// An explicit budget wins over the effort mapping.
	if got := (&ReasoningConfig{Effort: "low", BudgetTokens: 30000}).budget(); got != 30000 {
		t.Errorf("explicit budget = %d, want 30000", got)
	}
	// Anthropic's floor is 1024; anything smaller would be rejected.
	if got := (&ReasoningConfig{BudgetTokens: 10}).budget(); got != 1024 {
		t.Errorf("budget below the floor = %d, want 1024", got)
	}
}

func TestReasoning_AnthropicSendsThinkingAndKeepsMaxTokensAbove(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, anthropicSSEDone)
	}))
	defer srv.Close()

	c := NewAnthropicClient(LLMConfig{
		APIBase: srv.URL, APIKey: "k", Model: "claude-sonnet-4-5",
		MaxTokens: 4096, // smaller than the budget below
		Reasoning: &ReasoningConfig{BudgetTokens: 8000},
	})
	if _, err := c.Complete(context.Background(), CompleteRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}); err != nil {
		t.Fatal(err)
	}

	var sent map[string]any
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatal(err)
	}
	th, ok := sent["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("thinking = %v, want an object", sent["thinking"])
	}
	if th["type"] != "enabled" || th["budget_tokens"] != float64(8000) {
		t.Errorf("thinking = %v, want enabled/8000", th)
	}
	// Anthropic rejects the request unless max_tokens exceeds the budget.
	if mt, _ := sent["max_tokens"].(float64); mt <= 8000 {
		t.Errorf("max_tokens = %v, want > 8000", mt)
	}
}

func TestReasoning_AnthropicThinkingDeltaBecomesAReasoningEvent(t *testing.T) {
	// Without this the model's visible answer looks empty while it thinks:
	// the thinking blocks were parsed and dropped.
	const sse = `event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"weighing options"}}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"answer"}}

event: message_stop
data: {"type":"message_stop"}

`
	var reasoning, text string
	for ev := range ParseAnthropicSSEStream(context.Background(), strings.NewReader(sse)) {
		switch ev.Kind {
		case StreamEventReasoningDelta:
			reasoning += ev.Content
		case StreamEventMessageDelta:
			text += ev.Content
		}
	}
	if reasoning != "weighing options" {
		t.Errorf("reasoning = %q, want the thinking text", reasoning)
	}
	if text != "answer" {
		t.Errorf("text = %q, want only the answer — thinking must not leak into content", text)
	}
}
