package llm

import "strings"

// modelWindow maps a model-name substring to its context window in tokens.
//
// Cloud providers almost never report a context window through
// /v1/models (unlike vLLM's max_model_len or LM Studio's
// max_context_length), so DiscoverModelLimits comes back empty for them and
// the agent falls back to the flat limits.context_kb budget — historically
// 128 KB ≈ 30k tokens on a 200k model. This table is the static fallback so
// history budgeting tracks the real window.
//
// Matching is on the model string only (not the provider): gateways such as
// OpenRouter or Together prefix the vendor ("anthropic/claude-sonnet-4.5"),
// so a substring match on the family name works across all of them.
// Order matters — the first matching prefix entry wins, so more specific
// families must come first.
var modelWindows = []struct {
	match  string
	tokens int
}{
	// Anthropic
	{"claude-3-haiku", 200000},
	{"claude-3-opus", 200000},
	{"claude-3-5", 200000},
	{"claude-3.5", 200000},
	{"claude-3-7", 200000},
	{"claude-3.7", 200000},
	{"claude-haiku-4", 200000},
	{"claude-sonnet-4", 200000},
	{"claude-opus-4", 200000},
	{"claude-sonnet-5", 200000},
	{"claude-opus-5", 200000},
	{"claude-haiku-5", 200000},
	{"claude-fable-5", 200000},
	{"claude", 200000},

	// OpenAI
	{"gpt-5", 400000},
	{"gpt-4.1", 1047576},
	{"gpt-4o", 128000},
	{"gpt-4-turbo", 128000},
	{"gpt-4", 8192},
	{"gpt-3.5", 16385},
	{"o4-mini", 200000},
	{"o3", 200000},
	{"o1", 200000},

	// Google
	{"gemini-1.5-pro", 2097152},
	{"gemini-1.5", 1048576},
	{"gemini-2", 1048576},
	{"gemini-3", 1048576},
	{"gemini", 1048576},

	// DeepSeek
	{"deepseek-reasoner", 131072},
	{"deepseek-chat", 131072},
	{"deepseek-v3", 131072},
	{"deepseek-r1", 131072},
	{"deepseek", 131072},

	// xAI
	{"grok-4", 256000},
	{"grok-3", 131072},
	{"grok-code", 256000},
	{"grok", 131072},

	// Moonshot / Kimi
	{"kimi-k2", 131072},
	{"moonshot", 131072},
	{"kimi", 131072},

	// Mistral
	{"codestral", 262144},
	{"mistral-large", 131072},
	{"magistral", 131072},
	{"devstral", 131072},
	{"ministral", 131072},
	{"mistral", 32768},

	// Common open-weight families served by gateways and local runtimes.
	{"qwen3-coder", 262144},
	{"qwen3", 131072},
	{"qwen2.5-coder", 32768},
	{"qwen2.5", 32768},
	{"llama-4", 1048576},
	{"llama-3.3", 131072},
	{"llama-3.1", 131072},
	{"glm-4", 131072},
	{"minimax", 1000000},
	{"gpt-oss", 131072},
}

// ModelContextWindow returns the known context window (in tokens) for a model
// name, or 0 when the model is unknown. The lookup is case-insensitive and
// tolerates gateway prefixes ("openrouter/anthropic/claude-...") and
// suffixes (":free", "-latest", a trailing date, a quantization tag).
//
// The curated table above wins over the models.dev snapshot, which is only
// consulted for families the table does not name. models.dev reports the
// vendor's *advertised* maximum, including tiers that need a request header
// Orchestra does not send (Anthropic's 1M beta is the live example): trusting
// it for budgeting would size history past what the API will accept. The
// snapshot's job here is coverage of the long tail, not correction.
func ModelContextWindow(model string) int {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return 0
	}
	for _, w := range modelWindows {
		if strings.Contains(m, w.match) {
			return w.tokens
		}
	}
	if mi, ok := LookupModelInfo(m); ok {
		return mi.ContextWindow
	}
	return 0
}

// CatalogModelLimits returns static limits for cfg's model when it is a known
// family, so callers have a window even when the server reports none.
func CatalogModelLimits(cfg LLMConfig) (ModelLimits, bool) {
	n := ModelContextWindow(cfg.Model)
	if n <= 0 {
		return ModelLimits{}, false
	}
	return ModelLimits{
		Model:         cfg.Model,
		ContextTokens: n,
		MaxTokensCap:  effectiveMaxTokens(1<<30, n),
		Source:        "static model catalog",
	}, true
}
