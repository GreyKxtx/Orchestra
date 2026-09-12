package core

import (
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
)

func fallbackTestCore() *Core {
	return &Core{cfg: &config.ProjectConfig{
		LLM: config.LLMConfig{
			APIBase:          "http://primary.invalid/v1",
			Model:            "main-model",
			FallbackProvider: "backup",
		},
		Providers: map[string]config.LLMConfig{
			"fast": {
				APIBase:          "http://fast.invalid/v1",
				Model:            "small-model",
				FallbackProvider: "backup",
			},
			"backup": {
				APIBase: "http://backup.invalid/v1",
				Model:   "standby-model",
			},
			"lonely": {
				APIBase: "http://lonely.invalid/v1",
				Model:   "no-standby",
			},
		},
	}}
}

// The default llm client is wrapped for failover at construction (core.go).
// A client resolved BY NAME — what "fast" compaction and every UI model switch
// use — went out bare, so picking a model in the UI silently dropped the
// standby the same config had asked for.
func TestResolveNamedClient_CarriesTheProvidersStandby(t *testing.T) {
	c := fallbackTestCore()

	client, provider, model, err := c.resolveNamedClient("fast", "", nil)
	if err != nil {
		t.Fatalf("resolveNamedClient: %v", err)
	}
	if provider != "fast" || model != "small-model" {
		t.Fatalf("resolved the wrong provider/model: %q %q", provider, model)
	}
	if _, ok := client.(*llm.FallbackClient); !ok {
		t.Fatalf("a named provider with fallback_provider must fail over, got %T", client)
	}
}

// A model override with no named provider still runs against the main
// endpoint, so it keeps the main config's standby.
func TestResolveNamedClient_ModelOverrideKeepsTheMainStandby(t *testing.T) {
	c := fallbackTestCore()

	client, _, model, err := c.resolveNamedClient("", "other-model", nil)
	if err != nil {
		t.Fatalf("resolveNamedClient: %v", err)
	}
	if model != "other-model" {
		t.Fatalf("model override lost: %q", model)
	}
	if _, ok := client.(*llm.FallbackClient); !ok {
		t.Fatalf("a model override must keep the main standby, got %T", client)
	}
}

// Wrapping must stay opt-in: a provider that names no standby is untouched,
// so no run pays a second dial timeout it never asked for.
func TestResolveNamedClient_NoStandbyStaysBare(t *testing.T) {
	c := fallbackTestCore()

	client, _, _, err := c.resolveNamedClient("lonely", "", nil)
	if err != nil {
		t.Fatalf("resolveNamedClient: %v", err)
	}
	if _, ok := client.(*llm.FallbackClient); ok {
		t.Fatal("a provider with no fallback_provider must not be wrapped")
	}
}

// The compaction client is resolved by name, so it inherits the same standby
// instead of taking the whole turn down with the cheap endpoint.
func TestCompactionClient_CarriesTheStandbyToo(t *testing.T) {
	c := fallbackTestCore()

	client, _ := c.compactionClientWithContext(nil)
	if client == nil {
		t.Fatal("providers.fast must supply a compaction client")
	}
	if _, ok := client.(*llm.FallbackClient); !ok {
		t.Fatalf("the compaction client must fail over, got %T", client)
	}
}
