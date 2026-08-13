package llm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/orchestra/orchestra/llm/lmstudio"
)

// ModelLimits is the server-advertised context window and a safe max_tokens cap.
type ModelLimits struct {
	Model         string
	ContextTokens int    // max_model_len / max_context_length from /v1/models
	MaxTokensCap  int    // largest completion budget that still leaves prompt room
	Source        string // e.g. "GET /v1/models"
}

// DiscoverModelLimits queries the OpenAI-compatible /v1/models (or LM Studio
// /api/v0/models) for the configured model's context window.
func DiscoverModelLimits(ctx context.Context, cfg LLMConfig) (ModelLimits, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.APIBase), "/")
	if base == "" {
		return ModelLimits{}, fmt.Errorf("api_base is empty")
	}
	// Bound the HTTP call even if parent ctx is long-lived.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
	}

	client := lmstudio.NewClient(base, cfg.APIKey)
	type result struct {
		n   int64
		err error
	}
	ch := make(chan result, 1)
	go func() {
		n, err := client.FindModelContext(cfg.Model)
		ch <- result{n, err}
	}()
	select {
	case <-ctx.Done():
		return ModelLimits{}, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return ModelLimits{}, r.err
		}
		if r.n <= 0 {
			return ModelLimits{Model: cfg.Model, Source: "GET /v1/models"}, nil
		}
		ctxTok := int(r.n)
		return ModelLimits{
			Model:         cfg.Model,
			ContextTokens: ctxTok,
			MaxTokensCap:  effectiveMaxTokens(1<<30, ctxTok), // clamp "infinite" want → cap only
			Source:        "GET /v1/models",
		}, nil
	}
}

// ApplyDiscoveredLimits reconciles server max_model_len with the user's
// configured num_ctx:
//
//   - unset num_ctx  → fill with server max
//   - user ≤ server  → keep user, but only for local providers (intentional
//     smaller window / RAM); cloud providers trust the server-reported window
//     so a stale num_ctx from an old local setup cannot shrink e.g. a 1M model
//   - user > server  → clamp down to server (avoid 400 overflow)
//
// Also clamps max_tokens to the effective window and syncs ModelPresets[model].NumCtx
// so EffectiveNumCtx and the LLM client stay aligned. Returns true if cfg changed.
func ApplyDiscoveredLimits(cfg *LLMConfig, lim ModelLimits) bool {
	if cfg == nil || lim.ContextTokens <= 0 {
		return false
	}
	changed := false
	if cfg.ExtraBody == nil {
		cfg.ExtraBody = map[string]any{}
	}

	user := userConfiguredNumCtx(cfg)
	target := lim.ContextTokens
	if user > 0 && user < lim.ContextTokens && providerHonorsUserWindow(cfg.Provider) {
		target = user
	}

	prevExtra := contextLenFromExtra(cfg.ExtraBody)
	if prevExtra != target {
		cfg.ExtraBody["num_ctx"] = target
		changed = true
	}

	if model := strings.TrimSpace(cfg.Model); model != "" {
		if cfg.ModelPresets == nil {
			cfg.ModelPresets = map[string]ModelPreset{}
		}
		p := cfg.ModelPresets[model]
		if p.NumCtx != int64(target) {
			p.NumCtx = int64(target)
			cfg.ModelPresets[model] = p
			changed = true
		}
	}

	clamped := effectiveMaxTokens(cfg.MaxTokens, target)
	if cfg.MaxTokens != clamped {
		cfg.MaxTokens = clamped
		changed = true
	}
	return changed
}

// providerHonorsUserWindow reports whether a user num_ctx below the server
// window should be kept. For local runtimes (ollama, lmstudio, vllm, custom)
// num_ctx reflects real RAM/KV-cache limits. For known cloud providers the
// value is usually a leftover from an earlier local setup, and the
// provider-reported context window is authoritative. Unknown/empty providers
// keep the conservative local behavior.
func providerHonorsUserWindow(provider string) bool {
	p := strings.TrimSpace(provider)
	if p == "" {
		return true
	}
	if cat, ok := FindCatalogProvider(p); ok {
		return cat.Local
	}
	return true
}

// userConfiguredNumCtx returns the operator's intended window: preset for the
// active model wins (same as ProjectConfig.EffectiveNumCtx), else extra_body.
func userConfiguredNumCtx(cfg *LLMConfig) int {
	if cfg == nil {
		return 0
	}
	model := strings.TrimSpace(cfg.Model)
	if model != "" && cfg.ModelPresets != nil {
		if p, ok := cfg.ModelPresets[model]; ok && p.NumCtx > 0 {
			return int(p.NumCtx)
		}
	}
	return contextLenFromExtra(cfg.ExtraBody)
}

// ClampMaxTokensAgainstContext is the exported form of effectiveMaxTokens.
func ClampMaxTokensAgainstContext(maxTokens, contextLen int) int {
	return effectiveMaxTokens(maxTokens, contextLen)
}
