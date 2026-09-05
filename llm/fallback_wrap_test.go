package llm

import "testing"

func fallbackRegistry() ProviderRegistry {
	return NewProviderRegistry(
		LLMConfig{APIBase: "http://localhost:8000/v1", Model: "local"},
		map[string]LLMConfig{
			"backup": {Provider: "openrouter", APIBase: "https://openrouter.ai/api/v1", Model: "anthropic/claude-sonnet-4.5"},
			"twin":   {Provider: "vllm", APIBase: "http://localhost:8000/v1", Model: "local"},
		},
	)
}

func TestMaybeWrapFallback_WrapsWhenTheProviderResolves(t *testing.T) {
	main := NewOpenAIClient(LLMConfig{APIBase: "http://localhost:8000/v1", Model: "local"})
	got := MaybeWrapFallback(main, fallbackRegistry(),
		LLMConfig{Provider: "vllm", APIBase: "http://localhost:8000/v1", FallbackProvider: "backup"}, nil)

	fb, ok := got.(*FallbackClient)
	if !ok {
		t.Fatalf("got %T, want *FallbackClient", got)
	}
	if fb.ActiveProvider() != "vllm" {
		t.Errorf("ActiveProvider = %q, want the primary's name", fb.ActiveProvider())
	}
}

func TestMaybeWrapFallback_LeavesTheClientAloneWhenItCannot(t *testing.T) {
	main := NewOpenAIClient(LLMConfig{APIBase: "http://localhost:8000/v1", Model: "local"})
	reg := fallbackRegistry()

	cases := map[string]LLMConfig{
		"not configured": {APIBase: "http://localhost:8000/v1"},
		"unknown name":   {APIBase: "http://localhost:8000/v1", FallbackProvider: "nope"},
		// A "fallback" on the same endpoint fails at the same moment as the
		// primary — wrapping would only add a second dial timeout per step.
		"same endpoint": {APIBase: "http://localhost:8000/v1", FallbackProvider: "twin"},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if got := MaybeWrapFallback(main, reg, cfg, nil); got != Client(main) {
				t.Errorf("got %T, want the unwrapped client", got)
			}
		})
	}
}

func TestAsOpenAIClient_SeesThroughWrappers(t *testing.T) {
	// Every logger attachment in core and cli type-asserts on the concrete
	// client. Wrapping it must not silently empty llm_log.jsonl.
	inner := NewOpenAIClient(LLMConfig{APIBase: "http://localhost:8000/v1", Model: "local"})
	wrapped := MaybeWrapFallback(inner, fallbackRegistry(),
		LLMConfig{APIBase: "http://localhost:8000/v1", FallbackProvider: "backup"}, nil)

	got, ok := AsOpenAIClient(wrapped)
	if !ok || got != inner {
		t.Fatalf("AsOpenAIClient through a fallback = %v/%v, want the inner client", got, ok)
	}
	if got, ok := AsOpenAIClient(NewRouterClient(inner, inner, 2048)); !ok || got != inner {
		t.Fatalf("AsOpenAIClient through a router = %v/%v", got, ok)
	}
	if got, ok := AsOpenAIClient(inner); !ok || got != inner {
		t.Fatalf("AsOpenAIClient on a bare client = %v/%v", got, ok)
	}
	if _, ok := AsOpenAIClient(NewAnthropicClient(LLMConfig{Model: "claude-sonnet-4-5"})); ok {
		t.Error("an Anthropic client is not an OpenAI client")
	}
}
