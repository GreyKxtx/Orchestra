package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
)

func newWebSearchRunner(t *testing.T, cfg config.WebSearchConfig) *Runner {
	t.Helper()
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{WebSearch: cfg})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func TestWebSearch_Tavily_Basic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["query"] != "golang context" {
			t.Errorf("unexpected query: %v", body["query"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"query": "golang context",
			"results": []map[string]any{
				{"title": "Go Context", "url": "https://pkg.go.dev/context", "content": "Package context...", "score": 0.95},
			},
		})
	}))
	defer srv.Close()

	r := newWebSearchRunner(t, config.WebSearchConfig{
		Provider:   "tavily",
		APIKey:     "test-key",
		MaxResults: 5,
	})
	r.webSearchTavilyEndpoint = srv.URL

	resp, err := r.WebSearch(context.Background(), WebSearchRequest{Query: "golang context"})
	if err != nil {
		t.Fatalf("WebSearch: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}
	if resp.Results[0].Title != "Go Context" {
		t.Errorf("unexpected title: %q", resp.Results[0].Title)
	}
}

func TestWebSearch_NoProviderConfigured(t *testing.T) {
	r := newWebSearchRunner(t, config.WebSearchConfig{})
	_, err := r.WebSearch(context.Background(), WebSearchRequest{Query: "test"})
	if err == nil {
		t.Fatal("expected error when no provider configured")
	}
}

func TestWebSearch_EmptyQueryFails(t *testing.T) {
	r := newWebSearchRunner(t, config.WebSearchConfig{Provider: "tavily", APIKey: "k"})
	_, err := r.WebSearch(context.Background(), WebSearchRequest{Query: ""})
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestWebSearch_Brave_Basic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Subscription-Token") == "" {
			t.Error("missing X-Subscription-Token header")
		}
		if r.URL.Query().Get("q") != "rust ownership" {
			t.Errorf("unexpected query: %q", r.URL.Query().Get("q"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"web": map[string]any{
				"results": []map[string]any{
					{"title": "Rust Ownership", "url": "https://doc.rust-lang.org/book/ch04-01-what-is-ownership.html", "description": "What is Ownership?"},
				},
			},
		})
	}))
	defer srv.Close()

	r := newWebSearchRunner(t, config.WebSearchConfig{
		Provider:   "brave",
		APIKey:     "brave-test-key",
		MaxResults: 5,
	})
	r.webSearchBraveEndpoint = srv.URL

	resp, err := r.WebSearch(context.Background(), WebSearchRequest{Query: "rust ownership"})
	if err != nil {
		t.Fatalf("WebSearch brave: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}
	if resp.Results[0].Title != "Rust Ownership" {
		t.Errorf("unexpected title: %q", resp.Results[0].Title)
	}
}

func TestWebSearch_NoAPIKey(t *testing.T) {
	r := newWebSearchRunner(t, config.WebSearchConfig{Provider: "tavily"}) // no APIKey
	_, err := r.WebSearch(context.Background(), WebSearchRequest{Query: "test"})
	if err == nil {
		t.Fatal("expected error when no API key configured")
	}
}

func TestWebSearch_UnknownProvider(t *testing.T) {
	r := newWebSearchRunner(t, config.WebSearchConfig{Provider: "bing", APIKey: "k"})
	_, err := r.WebSearch(context.Background(), WebSearchRequest{Query: "test"})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}
