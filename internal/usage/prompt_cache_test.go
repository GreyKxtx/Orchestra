package usage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTracker_PromptCache_ReachesUsageJSONL(t *testing.T) {
	dir := t.TempDir()
	tr := NewTracker("r1", "session.turn", nil)
	tr.RecordCost("openrouter", "anthropic/claude-sonnet-5", 20_000, 300, 0)
	tr.RecordPromptCache("openrouter", "anthropic/claude-sonnet-5", 18_000, 1_500)
	tr.RecordCost("openrouter", "anthropic/claude-sonnet-5", 21_000, 200, 0)
	tr.RecordPromptCache("openrouter", "anthropic/claude-sonnet-5", 19_500, 0)

	rec, _, err := tr.Finalize(dir)
	if err != nil {
		t.Fatal(err)
	}

	// The field run's $2.18 turn could not be diagnosed after the fact because
	// usage.jsonl only knew the gross prompt size. The cache split has to be in
	// the durable record, not just in the live TUI event.
	if rec.Totals.CachedPromptTokens != 37_500 {
		t.Errorf("totals cached = %d, want 37500", rec.Totals.CachedPromptTokens)
	}
	if rec.Totals.CacheWriteTokens != 1_500 {
		t.Errorf("totals cache write = %d, want 1500", rec.Totals.CacheWriteTokens)
	}
	if rec.Totals.Calls != 2 {
		t.Errorf("cache counters must not count as extra calls, got %d", rec.Totals.Calls)
	}
	body, _ := os.ReadFile(filepath.Join(dir, ".orchestra", "usage.jsonl"))
	if !strings.Contains(string(body), `"cached_prompt_tokens":37500`) {
		t.Fatalf("usage.jsonl lacks cache counters: %s", body)
	}
}

func TestTracker_PromptCache_ZeroIsOmitted(t *testing.T) {
	dir := t.TempDir()
	tr := NewTracker("r1", "apply", nil)
	tr.Record("openai", "m", 10, 5)
	if _, _, err := tr.Finalize(dir); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, ".orchestra", "usage.jsonl"))
	// Local models never report a cache; their records should look as before.
	if strings.Contains(string(body), "cached_prompt_tokens") {
		t.Fatalf("zero cache counters must not clutter the record: %s", body)
	}
}

func TestAggregate_SumsPromptCacheAcrossRuns(t *testing.T) {
	dir := t.TempDir()
	for _, cached := range []int{18_000, 19_500} {
		tr := NewTracker("r", "session.turn", nil)
		tr.RecordCost("openrouter", "anthropic/claude-sonnet-5", 20_000, 200, 0)
		tr.RecordPromptCache("openrouter", "anthropic/claude-sonnet-5", cached, 100)
		if _, _, err := tr.Finalize(dir); err != nil {
			t.Fatal(err)
		}
	}
	records, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	per, totals := Aggregate(records)
	// `orchestra usage` is how the run gets read afterwards; it must add up the
	// cache split the same way it adds up the gross tokens.
	if totals.CachedPromptTokens != 37_500 || totals.CacheWriteTokens != 200 {
		t.Errorf("aggregate cache = %d/%d, want 37500/200", totals.CachedPromptTokens, totals.CacheWriteTokens)
	}
	if len(per) != 1 || per[0].CachedPromptTokens != 37_500 {
		t.Errorf("per-model cache = %+v", per)
	}
}

func TestTracker_PromptCache_NilReceiverIsNoop(t *testing.T) {
	var tr *Tracker
	tr.RecordPromptCache("a", "b", 1, 2)
}
