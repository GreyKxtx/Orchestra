package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// wireFields builds one request the way CompleteStream does and returns the
// budget and temperature fields that went on the wire.
func wireFields(t *testing.T, cfg LLMConfig) map[string]any {
	t.Helper()
	c := NewOpenAIClient(cfg)
	req := CompleteRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}}
	tok, err := c.maxTokensForRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := c.buildChatBody(req, tok, true)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	out := map[string]any{}
	for _, k := range []string{"max_tokens", "max_completion_tokens", "temperature"} {
		if v, ok := body[k]; ok {
			out[k] = v
		}
	}
	return out
}

func TestProviderSpec_EachProviderGetsItsOwnRequest(t *testing.T) {
	cases := []struct {
		name string
		cfg  LLMConfig
		want map[string]any
	}{
		{
			// OpenAI takes max_completion_tokens for every chat model; its
			// reasoning models refuse max_tokens outright.
			name: "openai chat model",
			cfg:  LLMConfig{Provider: "openai", APIBase: "https://api.openai.com/v1", Model: "gpt-4.1", MaxTokens: 2000, Temperature: 0.2},
			want: map[string]any{"max_completion_tokens": 2000.0, "temperature": 0.2},
		},
		{
			// …and refuse any temperature but the default.
			name: "openai reasoning model",
			cfg:  LLMConfig{Provider: "openai", APIBase: "https://api.openai.com/v1", Model: "o4-mini", MaxTokens: 2000, Temperature: 0.2},
			want: map[string]any{"max_completion_tokens": 2000.0},
		},
		{
			name: "gpt-5 through a gateway",
			cfg:  LLMConfig{Provider: "openrouter", APIBase: "https://openrouter.ai/api/v1", Model: "openai/gpt-5", MaxTokens: 2000, Temperature: 0.2},
			want: map[string]any{"max_tokens": 2000.0},
		},
		{
			// A hosted model with no max_tokens configured answers up to its
			// own ceiling: 4096 cut long answers short, and DeepSeek refuses
			// anything over 8192.
			name: "hosted provider, max_tokens unset",
			cfg:  LLMConfig{Provider: "deepseek", APIBase: "https://api.deepseek.com/v1", Model: "deepseek-chat", Temperature: 0.2},
			want: map[string]any{"temperature": 0.2},
		},
		{
			name: "hosted provider, max_tokens set",
			cfg:  LLMConfig{Provider: "deepseek", APIBase: "https://api.deepseek.com/v1", Model: "deepseek-chat", MaxTokens: 8000},
			want: map[string]any{"max_tokens": 8000.0},
		},
		{
			// A local vLLM speaks OpenAI's dialect and is configured as
			// provider openai: it is not OpenAI, and without a default it
			// falls back to ~50k and overflows a small window.
			name: "local server configured as openai",
			cfg:  LLMConfig{Provider: "openai", APIBase: "http://localhost:18020/v1", Model: "qwen3.8-27b", Temperature: 0.2},
			want: map[string]any{"max_tokens": float64(defaultMaxTokens), "temperature": 0.2},
		},
		{
			name: "azure reasoning deployment",
			cfg:  LLMConfig{Provider: "azure", APIBase: "https://acme.openai.azure.com", Model: "o3", MaxTokens: 3000, Temperature: 0.2},
			want: map[string]any{"max_completion_tokens": 3000.0},
		},
		{
			name: "azure chat deployment",
			cfg:  LLMConfig{Provider: "azure", APIBase: "https://acme.openai.azure.com", Model: "gpt-4o", MaxTokens: 3000},
			want: map[string]any{"max_tokens": 3000.0},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := wireFields(t, tc.cfg)
			if len(got) != len(tc.want) {
				t.Fatalf("wire fields = %v, want %v", got, tc.want)
			}
			for k, v := range tc.want {
				g, ok := got[k]
				if !ok {
					t.Fatalf("wire fields = %v, want %v", got, tc.want)
				}
				if gf, ok := g.(float64); ok {
					if wf := v.(float64); gf-wf > 1e-6 || wf-gf > 1e-6 {
						t.Fatalf("%s = %v, want %v", k, g, v)
					}
				}
			}
		})
	}
}

func TestProviderSpec_ModelsListingPerProvider(t *testing.T) {
	cases := []struct {
		provider, base string
		want           []string
	}{
		{"anthropic", "https://api.anthropic.com", []string{"https://api.anthropic.com/v1/models?limit=1000"}},
		{"google", "https://generativelanguage.googleapis.com/v1beta/openai", []string{"https://generativelanguage.googleapis.com/v1beta/openai/models"}},
		{"openai", "https://api.openai.com/v1", []string{"https://api.openai.com/v1/models"}},
		{"azure", "https://acme.openai.azure.com/openai", []string{"https://acme.openai.azure.com/openai/models?api-version=" + defaultAzureAPIVersion}},
		{"lmstudio", "http://localhost:1234", []string{"http://localhost:1234/v1/models", "http://localhost:1234/models"}},
		{"openai", "http://localhost:8000/v1", []string{"http://localhost:8000/v1/models"}},
	}
	for _, tc := range cases {
		got := specFor(tc.provider, tc.base).modelsURLs(tc.base, "")
		if len(got) != len(tc.want) {
			t.Errorf("%s %s: %v, want %v", tc.provider, tc.base, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s %s: %v, want %v", tc.provider, tc.base, got, tc.want)
			}
		}
	}
}

func TestOpenAIReasoningModel(t *testing.T) {
	for m, want := range map[string]bool{
		"o1": true, "o3-mini": true, "o4-mini": true, "o1-preview": true,
		"gpt-5": true, "gpt-5-mini": true, "openai/gpt-5.1": true, "openai/o3": true,
		"gpt-4o": false, "gpt-4.1": false, "omni-moderation": false, "claude-sonnet-5-5": false, "": false,
	} {
		if got := openAIReasoningModel(m); got != want {
			t.Errorf("openAIReasoningModel(%q) = %v, want %v", m, got, want)
		}
	}
}

// Anthropic's temperature setting did nothing: it was never sent.
func TestAnthropic_TemperatureIsSentWithoutThinking(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, anthropicSSEDone)
	}))
	defer srv.Close()

	send := func(cfg LLMConfig) map[string]any {
		t.Helper()
		c := NewAnthropicClient(cfg)
		if _, err := c.Complete(context.Background(), CompleteRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}}); err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(body, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}

	plain := send(LLMConfig{APIBase: srv.URL, APIKey: "k", Model: "claude-sonnet-4-5", MaxTokens: 1024, Temperature: 0.3})
	if v, ok := plain["temperature"].(float64); !ok || v < 0.29 || v > 0.31 {
		t.Errorf("temperature = %v, want 0.3", plain["temperature"])
	}

	thinking := send(LLMConfig{APIBase: srv.URL, APIKey: "k", Model: "claude-sonnet-4-5", MaxTokens: 1024, Temperature: 0.3,
		Reasoning: &ReasoningConfig{Effort: "low"}})
	if _, ok := thinking["temperature"]; ok {
		t.Errorf("temperature sent with thinking on: Anthropic refuses the pair")
	}
}
