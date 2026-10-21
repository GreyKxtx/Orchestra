package llm_test

import (
	"testing"

	"github.com/orchestra/orchestra/llm"
)

func TestNewClient_DefaultIsOpenAI(t *testing.T) {
	c := llm.NewClient(llm.LLMConfig{Model: "m"})
	if _, ok := c.(*llm.OpenAIClient); !ok {
		t.Fatalf("expected *OpenAIClient for empty provider, got %T", c)
	}
}

func TestNewClient_OpenAI(t *testing.T) {
	c := llm.NewClient(llm.LLMConfig{Provider: "openai", Model: "m"})
	if _, ok := c.(*llm.OpenAIClient); !ok {
		t.Fatalf("expected *OpenAIClient, got %T", c)
	}
}

func TestNewClient_Anthropic(t *testing.T) {
	c := llm.NewClient(llm.LLMConfig{Provider: "anthropic", Model: "m"})
	if _, ok := c.(*llm.AnthropicClient); !ok {
		t.Fatalf("expected *AnthropicClient, got %T", c)
	}
}

func TestNewClient_CaseInsensitive(t *testing.T) {
	c := llm.NewClient(llm.LLMConfig{Provider: "Anthropic", Model: "m"})
	if _, ok := c.(*llm.AnthropicClient); !ok {
		t.Fatalf("expected *AnthropicClient for 'Anthropic' (mixed case), got %T", c)
	}
}
