package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
)

func TestDiscoverModelLimits_VLLMMaxModelLen(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v0/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"Qwen/Qwen3.6-27B-FP8","max_model_len":51200}]}`))
	}))
	t.Cleanup(srv.Close)

	lim, err := DiscoverModelLimits(context.Background(), config.LLMConfig{
		APIBase: srv.URL,
		Model:   "qwen/qwen3.6-27b-fp8",
	})
	if err != nil {
		t.Fatal(err)
	}
	if lim.ContextTokens != 51200 {
		t.Fatalf("ContextTokens=%d", lim.ContextTokens)
	}
	if lim.MaxTokensCap <= 0 || lim.MaxTokensCap >= 51200 {
		t.Fatalf("MaxTokensCap=%d want in (0,51200)", lim.MaxTokensCap)
	}
}

func TestApplyDiscoveredLimits_ClampsMaxTokens(t *testing.T) {
	cfg := &config.LLMConfig{
		MaxTokens: 50000,
		ExtraBody: map[string]any{"num_ctx": 20000},
	}
	changed := ApplyDiscoveredLimits(cfg, ModelLimits{ContextTokens: 51200, MaxTokensCap: 38400})
	if !changed {
		t.Fatal("expected change")
	}
	if cfg.ExtraBody["num_ctx"] != 51200 {
		t.Fatalf("num_ctx=%v", cfg.ExtraBody["num_ctx"])
	}
	// No half-window tax: 50000 fits under 51200 - MinCompletionTokens.
	if cfg.MaxTokens != 50000 {
		t.Fatalf("MaxTokens=%d want 50000 (no half-reserve)", cfg.MaxTokens)
	}

	cfg2 := &config.LLMConfig{MaxTokens: 80000}
	if !ApplyDiscoveredLimits(cfg2, ModelLimits{ContextTokens: 51200}) {
		t.Fatal("expected clamp of oversize max_tokens")
	}
	want := 51200 - MinCompletionTokens
	if cfg2.MaxTokens != want {
		t.Fatalf("MaxTokens=%d want %d", cfg2.MaxTokens, want)
	}
}
