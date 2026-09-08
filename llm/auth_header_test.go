package llm

import (
	"errors"
	"net/http"
	"testing"
)

func TestSetAuthHeader_PrefersTokenSourceOverAPIKey(t *testing.T) {
	c := NewOpenAIClient(LLMConfig{
		APIBase:     "https://api.example.com/v1",
		APIKey:      "static-key",
		TokenSource: func() (string, error) { return "fresh-bearer", nil },
	})
	h := http.Header{}
	if err := c.setAuthHeader(h); err != nil {
		t.Fatalf("setAuthHeader: %v", err)
	}
	if got := h.Get("Authorization"); got != "Bearer fresh-bearer" {
		t.Fatalf("Authorization = %q, want the token source's bearer", got)
	}
}

func TestSetAuthHeader_FallsBackToAPIKeyWithoutTokenSource(t *testing.T) {
	c := NewOpenAIClient(LLMConfig{APIBase: "https://api.example.com/v1", APIKey: "static-key"})
	h := http.Header{}
	if err := c.setAuthHeader(h); err != nil {
		t.Fatalf("setAuthHeader: %v", err)
	}
	if got := h.Get("Authorization"); got != "Bearer static-key" {
		t.Fatalf("Authorization = %q, want the static key", got)
	}
}

func TestSetAuthHeader_SurfacesTokenSourceFailure(t *testing.T) {
	boom := errors.New("refresh failed: run: orchestra auth login corp")
	c := NewOpenAIClient(LLMConfig{
		APIBase:     "https://api.example.com/v1",
		APIKey:      "static-key",
		TokenSource: func() (string, error) { return "", boom },
	})
	h := http.Header{}
	err := c.setAuthHeader(h)
	if err == nil {
		t.Fatal("expected the token source's error to surface")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want it to wrap the token source's error", err)
	}
	// A failed refresh must not silently fall back to the static key: that
	// would send a credential the user thought they had replaced.
	if got := h.Get("Authorization"); got != "" {
		t.Fatalf("Authorization = %q, want no header set on failure", got)
	}
}

func TestSetAuthHeader_AzureUsesAPIKeyHeaderWithTokenSource(t *testing.T) {
	// Provider: "azure" is what selects the Azure dialect -- azureFromConfig
	// (llm/azure.go:19) keys off the provider name or an explicit azure:
	// block, not off the endpoint's hostname.
	c := NewOpenAIClient(LLMConfig{
		Provider:    "azure",
		APIBase:     "https://example.openai.azure.com",
		Model:       "gpt-4o",
		TokenSource: func() (string, error) { return "fresh-bearer", nil },
	})
	h := http.Header{}
	if err := c.setAuthHeader(h); err != nil {
		t.Fatalf("setAuthHeader: %v", err)
	}
	if got := h.Get("api-key"); got != "fresh-bearer" {
		t.Fatalf("api-key = %q, want the token source's bearer", got)
	}
	if got := h.Get("Authorization"); got != "" {
		t.Fatalf("Azure must not receive an Authorization bearer, got %q", got)
	}
}

func TestAnthropicClient_UsesTokenSourceForAPIKeyHeader(t *testing.T) {
	c := NewAnthropicClient(LLMConfig{
		Provider:    "anthropic",
		APIKey:      "static-key",
		TokenSource: func() (string, error) { return "fresh-bearer", nil },
	})
	if c.tokenSource == nil {
		t.Fatal("constructor must carry TokenSource onto the client")
	}
	got, err := resolveBearer(c.tokenSource, c.apiKey)
	if err != nil {
		t.Fatal(err)
	}
	if got != "fresh-bearer" {
		t.Fatalf("resolved credential = %q, want the token source's bearer", got)
	}
}

// DiscoverAndApplyLimits rebuilds an LLMConfig from client fields; dropping
// the token source there would 401 every discovery call on an OAuth-only
// provider while the chat path worked, which is a confusing failure.
func TestDiscoverAndApplyLimits_CarriesTheTokenSource(t *testing.T) {
	var called bool
	c := NewOpenAIClient(LLMConfig{
		APIBase:     "http://127.0.0.1:1/v1", // refused fast; we only need the config build
		TokenSource: func() (string, error) { called = true; return "fresh-bearer", nil },
	})
	_, _ = c.DiscoverAndApplyLimits(t.Context())
	if !called {
		t.Fatal("DiscoverAndApplyLimits must pass the client's token source through")
	}
}
