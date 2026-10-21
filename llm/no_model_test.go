package llm

import (
	"context"
	"errors"
	"testing"
)

// A configuration that names no model gets no client to a guessed one: every
// call answers ErrNoModel, and no standby takes the request instead.
func TestAClientWithNoModelSaysSo(t *testing.T) {
	c := BuildClient(LLMConfig{Provider: "anthropic", APIBase: "https://api.anthropic.com", FallbackProvider: "other"}, ProviderRegistry{Named: map[string]LLMConfig{"other": {APIBase: "https://api.other.example", Model: "m"}}}, nil)
	if !HasNoModel(c) {
		t.Fatalf("a config with no model built %T", c)
	}
	if _, err := c.Complete(context.Background(), CompleteRequest{}); !errors.Is(err, ErrNoModel) {
		t.Fatalf("Complete = %v, want ErrNoModel", err)
	}
	if _, ok, _ := DiscoverLimits(context.Background(), c); ok {
		t.Fatal("a client with no model has no limits to discover")
	}
	if HasNoModel(NewClient(LLMConfig{APIBase: "http://localhost:1234/v1", Model: "qwen3"})) {
		t.Fatal("a client with a model reports none")
	}
}
