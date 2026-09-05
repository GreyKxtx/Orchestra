package core

import (
	"testing"

	"github.com/orchestra/orchestra/internal/usage"
)

func TestUsageSnapshotFrom_CarriesPromptCacheSplit(t *testing.T) {
	tr := usage.NewTracker("r", "session.turn", nil)
	tr.RecordCost("openrouter", "anthropic/claude-sonnet-5", 20_000, 300, 0.05)
	tr.RecordPromptCache("openrouter", "anthropic/claude-sonnet-5", 18_000, 700)

	snap := usageSnapshotFrom(tr)
	if snap == nil {
		t.Fatal("snapshot must not be nil for a non-empty tracker")
	}
	// The per-step event already carries the split; the per-turn result the
	// clients store must not drop it on the way.
	if snap.CachedPromptTokens != 18_000 || snap.CacheWriteTokens != 700 {
		t.Errorf("cache split = %d/%d, want 18000/700", snap.CachedPromptTokens, snap.CacheWriteTokens)
	}
	if snap.PromptTokens != 20_000 {
		t.Errorf("prompt tokens = %d, want 20000", snap.PromptTokens)
	}
}
