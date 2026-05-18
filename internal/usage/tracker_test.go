package usage

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestTracker_Record_AggregatesPerModel(t *testing.T) {
	tr := NewTracker("r1", "apply", nil)
	tr.Record("openai", "m-a", 100, 50)
	tr.Record("openai", "m-a", 30, 10)
	tr.Record("openai", "m-b", 5, 5)

	snap := tr.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("want 2 entries, got %d", len(snap))
	}
	var a *Entry
	for i := range snap {
		if snap[i].Model == "m-a" {
			a = &snap[i]
		}
	}
	if a == nil || a.Calls != 2 || a.PromptTokens != 130 || a.CompletionTokens != 60 || a.TotalTokens != 190 {
		t.Fatalf("aggregation wrong: %+v", a)
	}
}

func TestTracker_Pricing_AccumulatesCost(t *testing.T) {
	tr := NewTracker("r1", "apply", Pricing{
		"openai": {
			"m-a": ModelPricing{InputPer1M: 1.0, OutputPer1M: 2.0},
		},
	})
	tr.Record("openai", "m-a", 1_000_000, 500_000) // $1.00 + $1.00 = $2.00
	_, _, _, _, cost := tr.Total()
	if cost < 1.999 || cost > 2.001 {
		t.Fatalf("cost = %v, want ~2.0", cost)
	}
}

func TestTracker_NilReceiver_IsNoop(t *testing.T) {
	var tr *Tracker
	tr.Record("a", "b", 1, 2)
	if !tr.Empty() {
		t.Fatal("nil tracker should report empty")
	}
}

func TestTracker_ConcurrentRecord(t *testing.T) {
	tr := NewTracker("r", "apply", nil)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr.Record("openai", "x", 1, 1)
		}()
	}
	wg.Wait()
	calls, prompt, completion, _, _ := tr.Total()
	if calls != 50 || prompt != 50 || completion != 50 {
		t.Fatalf("lost updates: calls=%d prompt=%d completion=%d", calls, prompt, completion)
	}
}

func TestTracker_Finalize_AppendsJSONL(t *testing.T) {
	dir := t.TempDir()
	tr := NewTracker("r1", "apply", nil)
	tr.Record("openai", "m", 10, 5)
	rec, path, err := tr.Finalize(dir)
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil || path == "" {
		t.Fatal("expected non-nil rec + path")
	}
	body, _ := os.ReadFile(filepath.Join(dir, ".orchestra", "usage.jsonl"))
	if !strings.Contains(string(body), `"prompt_tokens":10`) {
		t.Fatalf("usage.jsonl missing record: %s", body)
	}

	// Second Finalize call on same tracker re-appends — append semantics.
	tr.Record("openai", "m", 1, 1)
	if _, _, err := tr.Finalize(dir); err != nil {
		t.Fatal(err)
	}
	body2, _ := os.ReadFile(filepath.Join(dir, ".orchestra", "usage.jsonl"))
	if lines := strings.Count(string(body2), "\n"); lines != 2 {
		t.Fatalf("want 2 lines, got %d: %s", lines, body2)
	}
}

func TestLoad_Aggregate_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	tr := NewTracker("r1", "apply", nil)
	tr.Record("openai", "m-a", 100, 50)
	tr.Record("openai", "m-b", 200, 100)
	if _, _, err := tr.Finalize(dir); err != nil {
		t.Fatal(err)
	}
	records, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("want 1 record, got %d", len(records))
	}
	per, totals := Aggregate(records)
	if len(per) != 2 {
		t.Fatalf("want 2 per-model entries, got %d", len(per))
	}
	if totals.PromptTokens != 300 || totals.CompletionTokens != 150 || totals.TotalTokens != 450 {
		t.Fatalf("totals wrong: %+v", totals)
	}
}

func TestLoad_MissingFile_ReturnsNil(t *testing.T) {
	recs, err := Load(t.TempDir())
	if err != nil || recs != nil {
		t.Fatalf("expected nil/nil, got %v/%v", recs, err)
	}
}

func TestSummaryLine_WithAndWithoutCost(t *testing.T) {
	tr := NewTracker("r", "apply", nil)
	tr.Record("openai", "m", 100, 50)
	got := tr.SummaryLine()
	if !strings.Contains(got, "100 in + 50 out = 150") {
		t.Fatalf("missing token counts: %q", got)
	}
	if strings.Contains(got, "$") {
		t.Fatalf("should not have cost without pricing: %q", got)
	}

	tr2 := NewTracker("r", "apply", Pricing{"openai": {"m": ModelPricing{InputPer1M: 10, OutputPer1M: 20}}})
	tr2.Record("openai", "m", 1_000_000, 500_000)
	got2 := tr2.SummaryLine()
	if !strings.Contains(got2, "$") {
		t.Fatalf("expected cost suffix: %q", got2)
	}
}
