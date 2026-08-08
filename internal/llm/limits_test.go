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

func TestApplyDiscoveredLimits_KeepsUserBelowServer(t *testing.T) {
	cfg := &config.LLMConfig{
		Model:     "qwen/qwen3.6-27b",
		MaxTokens: 3000,
		ExtraBody: map[string]any{"num_ctx": 20000},
		ModelPresets: map[string]config.ModelPreset{
			"qwen/qwen3.6-27b": {NumCtx: 20000},
		},
	}
	_ = ApplyDiscoveredLimits(cfg, ModelLimits{ContextTokens: 51200, MaxTokensCap: 38400})
	if cfg.ExtraBody["num_ctx"] != 20000 {
		t.Fatalf("num_ctx=%v want 20000 (keep intentional lower window)", cfg.ExtraBody["num_ctx"])
	}
	if cfg.ModelPresets["qwen/qwen3.6-27b"].NumCtx != 20000 {
		t.Fatalf("preset NumCtx=%d want 20000", cfg.ModelPresets["qwen/qwen3.6-27b"].NumCtx)
	}
	if cfg.MaxTokens != 3000 {
		t.Fatalf("MaxTokens=%d want 3000 (under 20%% of user window)", cfg.MaxTokens)
	}
}

func TestApplyDiscoveredLimits_ClampsUserAboveServer(t *testing.T) {
	cfg := &config.LLMConfig{
		Model:     "m",
		MaxTokens: 8192,
		ExtraBody: map[string]any{"num_ctx": 128000},
		ModelPresets: map[string]config.ModelPreset{
			"m": {NumCtx: 128000},
		},
	}
	if !ApplyDiscoveredLimits(cfg, ModelLimits{ContextTokens: 51200}) {
		t.Fatal("expected clamp of oversize num_ctx")
	}
	if cfg.ExtraBody["num_ctx"] != 51200 {
		t.Fatalf("num_ctx=%v want 51200", cfg.ExtraBody["num_ctx"])
	}
	if cfg.ModelPresets["m"].NumCtx != 51200 {
		t.Fatalf("preset NumCtx=%d want 51200", cfg.ModelPresets["m"].NumCtx)
	}
}

func TestApplyDiscoveredLimits_FillsWhenUnset(t *testing.T) {
	cfg := &config.LLMConfig{Model: "m", MaxTokens: 80000}
	if !ApplyDiscoveredLimits(cfg, ModelLimits{ContextTokens: 51200}) {
		t.Fatal("expected fill of missing num_ctx + clamp max_tokens")
	}
	if cfg.ExtraBody["num_ctx"] != 51200 {
		t.Fatalf("num_ctx=%v want 51200", cfg.ExtraBody["num_ctx"])
	}
	want := 51200 / 5 // ~20% completion cap
	if cfg.MaxTokens != want {
		t.Fatalf("MaxTokens=%d want %d", cfg.MaxTokens, want)
	}
	if cfg.ModelPresets["m"].NumCtx != 51200 {
		t.Fatalf("preset NumCtx=%d want 51200", cfg.ModelPresets["m"].NumCtx)
	}
}

func TestApplyDiscoveredLimits_ClampsMaxTokensToEffectiveWindow(t *testing.T) {
	cfg := &config.LLMConfig{
		MaxTokens: 50000,
		ExtraBody: map[string]any{"num_ctx": 20000},
	}
	_ = ApplyDiscoveredLimits(cfg, ModelLimits{ContextTokens: 51200, MaxTokensCap: 38400})
	// Window stays 20000 → max_tokens must fit that window, not the server max.
	want := 20000 / 5
	if cfg.MaxTokens != want {
		t.Fatalf("MaxTokens=%d want %d (clamped to user window)", cfg.MaxTokens, want)
	}
}
