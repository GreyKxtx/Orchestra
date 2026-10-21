package config

import "testing"

// llm:'s max_tokens and temperature are tuned for the local model. A hosted
// provider left without its own used to inherit them: Claude capped at 4096,
// and a temperature OpenAI's reasoning models refuse.
func TestFindProvider_HostedProviderDoesNotInheritLocalTuning(t *testing.T) {
	c := &ProjectConfig{
		LLM: LLMConfig{APIBase: "http://localhost:1234/v1", Model: "qwen", MaxTokens: 4096, Temperature: 0.7},
		Providers: map[string]LLMConfig{
			"anthropic": {Provider: "anthropic", APIBase: "https://api.anthropic.com", Model: "claude-sonnet-5-5"},
			"openai":    {Provider: "openai", APIBase: "https://api.openai.com/v1", Model: "gpt-5"},
			"vllm":      {Provider: "openai", APIBase: "http://localhost:18020/v1", Model: "qwen3.8-27b"},
		},
	}
	for _, name := range []string{"anthropic", "openai"} {
		p, ok := c.FindProvider(name)
		if !ok {
			t.Fatalf("%s not found", name)
		}
		if p.MaxTokens != 0 || p.Temperature != 0 {
			t.Errorf("%s inherited max_tokens=%d temperature=%v from the local llm:", name, p.MaxTokens, p.Temperature)
		}
	}
	// A local server still inherits: without max_tokens vLLM falls back to ~50k.
	p, _ := c.FindProvider("vllm")
	if p.MaxTokens != 4096 || p.Temperature != 0.7 {
		t.Errorf("vllm: max_tokens=%d temperature=%v, want 4096 and 0.7 inherited", p.MaxTokens, p.Temperature)
	}
}
