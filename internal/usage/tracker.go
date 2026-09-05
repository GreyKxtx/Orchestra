// Package usage tracks LLM token consumption and (optionally) USD cost per run.
//
// A Tracker accumulates per-(provider, model) totals during a single run and
// appends a JSONL record to .orchestra/usage.jsonl on Finalize. Tracker is
// concurrency-safe: subagents can share one Tracker across goroutines.
//
// Cost calculation is opt-in via Pricing. When no pricing entry exists for a
// (provider, model) pair, Cost stays zero and only token totals are reported.
package usage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Tracker accumulates token totals per (provider, model) for one run.
type Tracker struct {
	mu      sync.Mutex
	entries map[string]*Entry // key = provider+"|"+model
	pricing Pricing
	// listPrice is the vendor list price for a model, when the caller
	// installed one (UseListPrices). It is a function rather than another
	// Pricing map so this package keeps no dependency on the model catalog.
	listPrice func(model string) (ModelPricing, bool)
	runID     string
	command   string
	startAt   time.Time
}

// Entry is a per-(provider,model) accumulator.
type Entry struct {
	Provider         string  `json:"provider"`
	Model            string  `json:"model"`
	Calls            int     `json:"calls"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	CostUSD          float64 `json:"cost_usd,omitempty"`
	// CachedPromptTokens is the part of PromptTokens the provider served from
	// its prompt cache; CacheWriteTokens is what it charged to populate it.
	// Both stay zero (and out of the record) for providers without a cache,
	// which is every local model.
	CachedPromptTokens int `json:"cached_prompt_tokens,omitempty"`
	CacheWriteTokens   int `json:"cache_write_tokens,omitempty"`
}

// add folds another accumulator into e, field by field. Every place that sums
// entries goes through here so a new counter cannot be summed in one path and
// dropped in another — which is how the cache split went missing from
// `orchestra usage` while present in the raw records.
func (e *Entry) add(o Entry) {
	e.Calls += o.Calls
	e.PromptTokens += o.PromptTokens
	e.CompletionTokens += o.CompletionTokens
	e.TotalTokens += o.TotalTokens
	e.CostUSD += o.CostUSD
	e.CachedPromptTokens += o.CachedPromptTokens
	e.CacheWriteTokens += o.CacheWriteTokens
}

// Pricing maps provider → model → per-1M-token rates in USD.
type Pricing map[string]map[string]ModelPricing

// ModelPricing is the USD price per 1,000,000 input/output tokens.
type ModelPricing struct {
	InputPer1M  float64 `yaml:"input_per_1m"  json:"input_per_1m"`
	OutputPer1M float64 `yaml:"output_per_1m" json:"output_per_1m"`
}

// NewTracker creates an empty tracker. runID is a free-form identifier
// (e.g. timestamp) recorded with each persisted record so users can group
// runs in usage.jsonl.
func NewTracker(runID, command string, pricing Pricing) *Tracker {
	return &Tracker{
		entries: make(map[string]*Entry),
		pricing: pricing,
		runID:   runID,
		command: command,
		startAt: time.Now(),
	}
}

// Record adds one completion's usage to the running totals.
// Safe to call from goroutines. A nil receiver is a no-op so callers don't
// have to check; this makes wiring through agent.Options safe even when
// telemetry is disabled.
func (t *Tracker) Record(provider, model string, prompt, completion int) {
	t.RecordCost(provider, model, prompt, completion, 0)
}

// RecordCost is Record with a provider-reported cost (USD). When the provider
// returns real cost (OpenRouter usage accounting), it wins over the local
// pricing table; zero falls back to the pricing calculation.
func (t *Tracker) RecordCost(provider, model string, prompt, completion int, providerCostUSD float64) {
	if t == nil {
		return
	}
	e := t.entry(provider, model)
	defer t.mu.Unlock()
	e.Calls++
	e.PromptTokens += prompt
	e.CompletionTokens += completion
	e.TotalTokens += prompt + completion
	if providerCostUSD > 0 {
		e.CostUSD += providerCostUSD
	} else if mp, ok := t.priceFor(provider, model); ok {
		e.CostUSD += float64(prompt)/1_000_000*mp.InputPer1M + float64(completion)/1_000_000*mp.OutputPer1M
	}
}

// UseListPrices installs a fallback price source (the built-in model
// catalogue) consulted only for pairs the user's own pricing table does not
// name. Precedence, most specific first: the provider's reported cost, the
// configured table, then this.
func (t *Tracker) UseListPrices(fn func(model string) (ModelPricing, bool)) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.listPrice = fn
}

// priceFor resolves one (provider, model) pair. Callers hold t.mu.
func (t *Tracker) priceFor(provider, model string) (ModelPricing, bool) {
	if mp, ok := lookupPrice(t.pricing, provider, model); ok {
		return mp, true
	}
	if t.listPrice != nil {
		return t.listPrice(model)
	}
	return ModelPricing{}, false
}

// RecordPromptCache adds the provider's prompt-cache split for one completion
// whose gross tokens were already passed to Record/RecordCost. It is a
// separate call, not an extra parameter, so recorders that predate the cache
// counters keep compiling; it does not count as a call of its own.
func (t *Tracker) RecordPromptCache(provider, model string, cachedPrompt, cacheWrite int) {
	if t == nil || (cachedPrompt == 0 && cacheWrite == 0) {
		return
	}
	e := t.entry(provider, model)
	defer t.mu.Unlock()
	e.CachedPromptTokens += cachedPrompt
	e.CacheWriteTokens += cacheWrite
}

// entry returns the accumulator for (provider, model), creating it on first
// use. It returns with t.mu held; the caller must unlock.
func (t *Tracker) entry(provider, model string) *Entry {
	if provider == "" {
		provider = "openai"
	}
	if model == "" {
		model = "unknown"
	}
	key := provider + "|" + model
	t.mu.Lock()
	e, ok := t.entries[key]
	if !ok {
		e = &Entry{Provider: provider, Model: model}
		t.entries[key] = e
	}
	return e
}

// Snapshot returns a sorted copy of current entries. Safe under contention.
func (t *Tracker) Snapshot() []Entry {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Entry, 0, len(t.entries))
	for _, e := range t.entries {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		return out[i].Model < out[j].Model
	})
	return out
}

// Total aggregates calls, tokens and cost across all entries.
func (t *Tracker) Total() (calls, prompt, completion, total int, costUSD float64) {
	sum := t.Totals()
	return sum.Calls, sum.PromptTokens, sum.CompletionTokens, sum.TotalTokens, sum.CostUSD
}

// Totals is Total as one Entry (Provider/Model "*"), including the
// prompt-cache counters that the positional form predates.
func (t *Tracker) Totals() Entry {
	sum := Entry{Provider: "*", Model: "*"}
	for _, e := range t.Snapshot() {
		sum.add(e)
	}
	return sum
}

// Empty reports whether no Record calls have arrived yet.
func (t *Tracker) Empty() bool {
	if t == nil {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.entries) == 0
}

// SummaryLine returns a one-line human summary, e.g.
// "tokens: 4823 in + 612 out = 5435 (2 calls) | $0.0231".
// When pricing is missing the cost suffix is omitted.
func (t *Tracker) SummaryLine() string {
	calls, prompt, completion, total, cost := t.Total()
	if calls == 0 {
		return ""
	}
	base := fmt.Sprintf("tokens: %d in + %d out = %d (%d call%s)",
		prompt, completion, total, calls, plural(calls))
	if cost > 0 {
		return fmt.Sprintf("%s | $%.4f", base, cost)
	}
	return base
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// Record persisted to .orchestra/usage.jsonl. One JSON object per line.
type persistedRecord struct {
	RunID      string  `json:"run_id"`
	Command    string  `json:"command,omitempty"`
	StartedAt  string  `json:"started_at"`
	FinishedAt string  `json:"finished_at"`
	DurationMS int64   `json:"duration_ms"`
	Entries    []Entry `json:"entries"`
	Totals     Entry   `json:"totals"`
}

// Finalize appends a JSONL record summarising this run to <projectRoot>/.orchestra/usage.jsonl.
// Returns the persisted record (useful for tests / TUI display) and the path written to.
// No-op (returns nil, "", nil) when the tracker has zero entries.
func (t *Tracker) Finalize(projectRoot string) (*persistedRecord, string, error) {
	if t == nil || t.Empty() {
		return nil, "", nil
	}
	entries := t.Snapshot()
	now := time.Now()
	rec := &persistedRecord{
		RunID:      t.runID,
		Command:    t.command,
		StartedAt:  t.startAt.UTC().Format(time.RFC3339Nano),
		FinishedAt: now.UTC().Format(time.RFC3339Nano),
		DurationMS: now.Sub(t.startAt).Milliseconds(),
		Entries:    entries,
		Totals:     t.Totals(),
	}

	dir := filepath.Join(projectRoot, ".orchestra")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, "", fmt.Errorf("usage: mkdir .orchestra: %w", err)
	}
	path := filepath.Join(dir, "usage.jsonl")
	// Unbounded-growth guard: rotate to usage.jsonl.1 past 5 MB (one old
	// generation kept). Best-effort — rotation failure must not lose the record.
	if info, statErr := os.Stat(path); statErr == nil && info.Size() >= 5<<20 {
		_ = os.Remove(path + ".1")
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, "", fmt.Errorf("usage: open %s: %w", path, err)
	}
	defer f.Close()

	data, err := json.Marshal(rec)
	if err != nil {
		return nil, "", fmt.Errorf("usage: marshal: %w", err)
	}
	data = append(data, '\n')
	if _, err := f.Write(data); err != nil {
		return nil, "", fmt.Errorf("usage: write %s: %w", path, err)
	}
	return rec, path, nil
}

func lookupPrice(p Pricing, provider, model string) (ModelPricing, bool) {
	if p == nil {
		return ModelPricing{}, false
	}
	if pm, ok := p[provider]; ok {
		if mp, ok := pm[model]; ok {
			return mp, true
		}
	}
	// Fallback: pricing under "default" provider key.
	if pm, ok := p["default"]; ok {
		if mp, ok := pm[model]; ok {
			return mp, true
		}
	}
	return ModelPricing{}, false
}
