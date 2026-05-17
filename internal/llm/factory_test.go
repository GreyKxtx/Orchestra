package llm_test

import (
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/llm"
)

func TestNewClient_DefaultIsOpenAI(t *testing.T) {
	c := llm.NewClient(config.LLMConfig{})
	if _, ok := c.(*llm.OpenAIClient); !ok {
		t.Fatalf("expected *OpenAIClient for empty provider, got %T", c)
	}
}

func TestNewClient_OpenAI(t *testing.T) {
	c := llm.NewClient(config.LLMConfig{Provider: "openai"})
	if _, ok := c.(*llm.OpenAIClient); !ok {
		t.Fatalf("expected *OpenAIClient, got %T", c)
	}
}

func TestNewClient_Anthropic(t *testing.T) {
	c := llm.NewClient(config.LLMConfig{Provider: "anthropic"})
	if _, ok := c.(*llm.AnthropicClient); !ok {
		t.Fatalf("expected *AnthropicClient, got %T", c)
	}
}

func TestNewClient_CaseInsensitive(t *testing.T) {
	c := llm.NewClient(config.LLMConfig{Provider: "Anthropic"})
	if _, ok := c.(*llm.AnthropicClient); !ok {
		t.Fatalf("expected *AnthropicClient for 'Anthropic' (mixed case), got %T", c)
	}
}
