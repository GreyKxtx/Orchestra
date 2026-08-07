package llm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/lmstudio"
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
func DiscoverModelLimits(ctx context.Context, cfg config.LLMConfig) (ModelLimits, error) {
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

// ApplyDiscoveredLimits writes server context into cfg (num_ctx) and clamps
// max_tokens so prompt+completion fit. Returns true if cfg changed.
func ApplyDiscoveredLimits(cfg *config.LLMConfig, lim ModelLimits) bool {
	if cfg == nil || lim.ContextTokens <= 0 {
		return false
	}
	changed := false
	if cfg.ExtraBody == nil {
		cfg.ExtraBody = map[string]any{}
	}
	prev := contextLenFromExtra(cfg.ExtraBody)
	if prev != lim.ContextTokens {
		cfg.ExtraBody["num_ctx"] = lim.ContextTokens
		changed = true
	}
	clamped := effectiveMaxTokens(cfg.MaxTokens, lim.ContextTokens)
	if cfg.MaxTokens != clamped {
		cfg.MaxTokens = clamped
		changed = true
	}
	return changed
}

// ClampMaxTokensAgainstContext is the exported form of effectiveMaxTokens.
func ClampMaxTokensAgainstContext(maxTokens, contextLen int) int {
	return effectiveMaxTokens(maxTokens, contextLen)
}
