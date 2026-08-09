package llm

import (
	"strings"
)

// NewClient creates an LLM client based on cfg.Provider.
// "anthropic" → AnthropicClient. Any other value → OpenAIClient.
func NewClient(cfg LLMConfig) Client {
	switch strings.ToLower(cfg.Provider) {
	case "anthropic":
		return NewAnthropicClient(cfg)
	default:
		return NewOpenAIClient(cfg)
	}
}
