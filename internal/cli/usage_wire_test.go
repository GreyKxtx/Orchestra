package cli

import (
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
)

// costOf runs one completion through a freshly wired tracker and returns the
// USD it recorded.
func costOf(t *testing.T, cfg *config.ProjectConfig, model string) float64 {
	t.Helper()
	tr := newUsageTracker("apply", cfg)
	tr.Record("p", model, 1_000_000, 0)
	_, _, _, _, cost := tr.Total()
	return cost
}

func TestNewUsageTracker_BuiltInPricesForAHostedProvider(t *testing.T) {
	cfg := &config.ProjectConfig{LLM: llm.LLMConfig{APIBase: "https://openrouter.ai/api/v1"}}
	// claude-sonnet-4-5 lists at $3 / 1M input; nothing is configured.
	if got := costOf(t, cfg, "anthropic/claude-sonnet-4.5"); got < 2.99 || got > 3.01 {
		t.Fatalf("cost = %v, want ~3.00 from the built-in table", got)
	}
}

func TestNewUsageTracker_NoBuiltInPricesForALocalEndpoint(t *testing.T) {
	// The same model name served from LM Studio costs nothing. Charging it
	// the hosted price would be an invented number in usage.jsonl.
	cfg := &config.ProjectConfig{LLM: llm.LLMConfig{APIBase: "http://localhost:1234/v1"}}
	if got := costOf(t, cfg, "anthropic/claude-sonnet-4.5"); got != 0 {
		t.Fatalf("cost = %v, want 0 for a local endpoint", got)
	}
}

func TestNewUsageTracker_ConfiguredPricingStillWins(t *testing.T) {
	cfg := &config.ProjectConfig{
		LLM: llm.LLMConfig{APIBase: "https://openrouter.ai/api/v1"},
		Pricing: config.PricingConfig{
			"p": {"anthropic/claude-sonnet-4.5": config.ModelPricing{InputPer1M: 1, OutputPer1M: 1}},
		},
	}
	if got := costOf(t, cfg, "anthropic/claude-sonnet-4.5"); got < 0.99 || got > 1.01 {
		t.Fatalf("cost = %v, want ~1.00 from the user's own table", got)
	}
}
