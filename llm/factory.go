package llm

import (
	"strings"
)

// NewClient creates an LLM client based on cfg.Provider.
// "anthropic" → AnthropicClient. Any other value → OpenAIClient.
//
// It returns a bare client: the standby from llm.fallback_provider and the
// fast/slow split from llm.router are wrappers, and a caller that builds a
// client by hand has to remember both. Prefer BuildClient.
func NewClient(cfg LLMConfig) Client {
	switch strings.ToLower(cfg.Provider) {
	case "anthropic":
		return NewAnthropicClient(cfg)
	default:
		return NewOpenAIClient(cfg)
	}
}

// BuildClient creates the client a configuration actually asks for: the
// provider, its log sink, the standby from fallback_provider, and the router.
//
// The wrappers were applied by hand at six call sites, and the hand-assembly
// drifted exactly as one would expect. The core applied the standby in three
// places and forgot it in the fourth (config_refresh), so llm.fallback_provider
// worked until the user saved a setting and then silently did nothing for the
// rest of the process. And llm.router was applied only under `apply` and
// `workflow`: on every core-backed surface — TUI, VS Code, web, desktop — the
// key passed validation and was ignored, which is worse than not supporting it.
//
// Order matters and is the one apply.go established: the standby wraps the
// provider, and the router wraps the standby, so a routed fast call still has
// somewhere to go when its endpoint is down.
//
// Both wrappers are no-ops unless configured, so this is safe wherever a bare
// NewClient stood before.
func BuildClient(cfg LLMConfig, reg ProviderRegistry, logger *Logger) Client {
	c := NewClient(cfg)
	attachLogger(c, logger)
	c = MaybeWrapFallback(c, reg, cfg, logger)
	return MaybeWrapRouter(c, reg, cfg.Router)
}
