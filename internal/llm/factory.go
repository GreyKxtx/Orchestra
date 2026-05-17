package llm

import (
	"strings"

	"github.com/orchestra/orchestra/internal/config"
)

// NewClient creates an LLM client based on cfg.Provider.
// "anthropic" → AnthropicClient. Any other value → OpenAIClient.
func NewClient(cfg config.LLMConfig) Client {
	switch strings.ToLower(cfg.Provider) {
	case "anthropic":
		return NewAnthropicClient(cfg)
	default:
		return NewOpenAIClient(cfg)
	}
}
