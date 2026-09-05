package llm

import "testing"

func TestModelInfo_VendorPrefixAndDateSuffix(t *testing.T) {
	// A gateway prefix and a dated snapshot id must both resolve to the
	// vendor's own row: this is the form OpenRouter and Anthropic actually
	// send, and it is the one the usage ledger records.
	for _, model := range []string{
		"claude-sonnet-4-5",
		"anthropic/claude-sonnet-4-5",
		"claude-sonnet-4-5-20250929",
		"anthropic/claude-sonnet-4.5",
		"  Anthropic/Claude-Sonnet-4.5  ",
	} {
		got, ok := LookupModelInfo(model)
		if !ok {
			t.Fatalf("ModelInfo(%q): not found", model)
		}
		if got.InputPer1M != 3 || got.OutputPer1M != 15 || got.CacheReadPer1M != 0.3 {
			t.Errorf("ModelInfo(%q) pricing = %v/%v/%v, want 3/15/0.3",
				model, got.InputPer1M, got.OutputPer1M, got.CacheReadPer1M)
		}
		if !got.Vision || !got.Tools || !got.Reasoning || !got.JSONSchema {
			t.Errorf("ModelInfo(%q) caps = %+v, want all four", model, got)
		}
	}
}

func TestModelInfo_DoesNotTrimIntoADifferentModel(t *testing.T) {
	// "gpt-4o-mini" must never fall back to "gpt-4o": same family, 16x the
	// input price. A wrong price is worse than no price.
	mini, ok := LookupModelInfo("gpt-4o-mini")
	if !ok {
		t.Fatal("gpt-4o-mini not found")
	}
	if mini.InputPer1M != 0.15 {
		t.Errorf("gpt-4o-mini input = %v, want 0.15", mini.InputPer1M)
	}
	// A model whose trailing segment is a word, not a date, has no fallback.
	if got, ok := LookupModelInfo("gpt-4o-imaginary"); ok {
		t.Errorf("gpt-4o-imaginary resolved to %+v, want a miss", got)
	}
}

func TestModelInfo_Misses(t *testing.T) {
	for _, model := range []string{"", "   ", "totally-unknown-model", "my-local-finetune"} {
		if _, ok := LookupModelInfo(model); ok {
			t.Errorf("ModelInfo(%q) reported a hit", model)
		}
	}
}

func TestModelInfo_NoToolsOrVisionIsReportedHonestly(t *testing.T) {
	got, ok := LookupModelInfo("amazon-nova-pro-v1")
	if !ok {
		t.Fatal("amazon-nova-pro-v1 not found")
	}
	if !got.Vision || !got.Tools {
		t.Errorf("nova-pro caps = %+v, want vision+tools", got)
	}
	if got.Reasoning || got.JSONSchema {
		t.Errorf("nova-pro caps = %+v, want no reasoning / no json_schema", got)
	}
}

func TestModelContextWindow_CuratedTableStillWins(t *testing.T) {
	// models.dev reports 1M for claude-sonnet-4-5 (the beta long-context
	// tier, which needs a header Orchestra does not send). Budgeting must
	// keep using the curated 200k or every long turn would 400.
	if got := ModelContextWindow("claude-sonnet-4-5"); got != 200000 {
		t.Fatalf("ModelContextWindow(claude-sonnet-4-5) = %d, want the curated 200000", got)
	}
}

func TestModelContextWindow_SnapshotFillsWhatTheCuratedTableMisses(t *testing.T) {
	// Before the snapshot this returned 0 and the agent fell back to the flat
	// limits.context_kb budget.
	if got := ModelContextWindow("amazon-nova-pro-v1"); got != 300000 {
		t.Fatalf("ModelContextWindow(amazon-nova-pro-v1) = %d, want 300000 from the snapshot", got)
	}
}
