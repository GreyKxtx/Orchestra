package llm

import "testing"

func TestModelContextWindow(t *testing.T) {
	cases := []struct {
		model string
		want  int
	}{
		{"claude-sonnet-4-5-20250929", 200000},
		{"anthropic/claude-opus-5", 200000},
		{"gpt-4o-mini", 128000},
		{"openai/gpt-5", 400000},
		{"gemini-2.5-pro", 1048576},
		{"deepseek-chat", 131072},
		{"qwen2.5-coder-7b", 32768},
		{"totally-unknown-model", 0},
		{"", 0},
	}
	for _, tc := range cases {
		if got := ModelContextWindow(tc.model); got != tc.want {
			t.Errorf("ModelContextWindow(%q)=%d want %d", tc.model, got, tc.want)
		}
	}
}

func TestCatalogModelLimits_FillsCloudWindow(t *testing.T) {
	cfg := LLMConfig{Provider: "anthropic", Model: "claude-sonnet-4-5", MaxTokens: 4096}
	lim, ok := CatalogModelLimits(cfg)
	if !ok || lim.ContextTokens != 200000 {
		t.Fatalf("catalog limits=%+v ok=%v", lim, ok)
	}
	if !ApplyDiscoveredLimits(&cfg, lim) {
		t.Fatal("ApplyDiscoveredLimits made no change")
	}
	if got := userConfiguredNumCtx(&cfg); got != 200000 {
		t.Fatalf("num_ctx after apply=%d want 200000", got)
	}
}

func TestResolveModelLimits_FallsBackToCatalog(t *testing.T) {
	// Unreachable api_base: discovery fails, the catalog still supplies a window.
	cfg := LLMConfig{Provider: "anthropic", APIBase: "http://127.0.0.1:1/v1", Model: "claude-sonnet-4-5"}
	lim, ok := ResolveModelLimits(t.Context(), &cfg)
	if !ok || lim.ContextTokens != 200000 || lim.Source != "static model catalog" {
		t.Fatalf("resolved=%+v ok=%v", lim, ok)
	}
}
