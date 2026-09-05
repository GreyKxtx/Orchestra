package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/usage"
)

// newUsageTracker constructs a tracker pre-loaded with the project's pricing
// table (if any). cmdLabel is recorded with the persisted record so users can
// distinguish apply / pipeline / from-plan runs in usage.jsonl.
func newUsageTracker(cmdLabel string, cfg *config.ProjectConfig) *usage.Tracker {
	pricing := toUsagePricing(cfg.Pricing)
	runID := time.Now().UTC().Format("20060102T150405.000Z")
	t := usage.NewTracker(runID, cmdLabel, pricing)
	// Behind the configured table (and behind any cost the provider reports
	// itself), fall back to the built-in models.dev snapshot.
	t.UseCatalogPrices(cfg.LLM.APIBase)
	return t
}

// toUsagePricing copies config.PricingConfig into the usage.Pricing shape.
// We don't share types because internal/config must not import internal/usage
// (config is imported by almost everything; cycles would proliferate).
func toUsagePricing(in config.PricingConfig) usage.Pricing {
	if len(in) == 0 {
		return nil
	}
	out := make(usage.Pricing, len(in))
	for provider, models := range in {
		bucket := make(map[string]usage.ModelPricing, len(models))
		for model, mp := range models {
			bucket[model] = usage.ModelPricing{
				InputPer1M:  mp.InputPer1M,
				OutputPer1M: mp.OutputPer1M,
			}
		}
		out[provider] = bucket
	}
	return out
}

// pluralS returns "s" when n != 1 (for English pluralisation in summary lines).
func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// providerLabelFor picks a stable label for usage records. When the user
// passed --provider <name>, we use that name (e.g. "fast"); otherwise we
// fall back to LLMConfig.Provider, then to "openai" so records remain
// queryable even when the field is empty in config.
func providerLabelFor(cfg *config.ProjectConfig, providerFlag string) string {
	if providerFlag != "" {
		return providerFlag
	}
	if cfg.LLM.Provider != "" {
		return cfg.LLM.Provider
	}
	return "openai"
}

// finalizeUsage flushes the run's totals to .orchestra/usage.jsonl and prints
// a one-line summary to stderr (so it doesn't pollute machine-parsable stdout
// from apply). Never fails the parent flow — telemetry is best-effort.
func finalizeUsage(tracker *usage.Tracker, cfg *config.ProjectConfig) {
	if tracker == nil || tracker.Empty() {
		return
	}
	rec, path, err := tracker.Finalize(cfg.ProjectRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[usage] WARN: failed to persist usage record: %v\n", err)
		return
	}
	if line := tracker.SummaryLine(); line != "" {
		fmt.Fprintf(os.Stderr, "[usage] %s\n", line)
	}
	if rec != nil && path != "" {
		fmt.Fprintf(os.Stderr, "[usage] appended to %s\n", path)
	}
}
