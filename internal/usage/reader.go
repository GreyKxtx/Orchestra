package usage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Record is the parsed shape of one line in usage.jsonl.
type Record struct {
	RunID      string  `json:"run_id"`
	Command    string  `json:"command,omitempty"`
	StartedAt  string  `json:"started_at"`
	FinishedAt string  `json:"finished_at"`
	DurationMS int64   `json:"duration_ms"`
	Entries    []Entry `json:"entries"`
	Totals     Entry   `json:"totals"`
}

// Load parses every line of <projectRoot>/.orchestra/usage.jsonl. Returns an
// empty slice when the file does not exist. Malformed lines are skipped with
// no error so partial files (e.g. crashed run) stay readable.
func Load(projectRoot string) ([]Record, error) {
	path := filepath.Join(projectRoot, ".orchestra", "usage.jsonl")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("usage: open %s: %w", path, err)
	}
	defer f.Close()

	var out []Record
	sc := bufio.NewScanner(f)
	// Allow large per-line records (long tool chains can push tokens > default scanner buffer).
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec Record
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		out = append(out, rec)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("usage: scan %s: %w", path, err)
	}
	return out, nil
}

// Aggregate sums every record by (provider, model). The returned entries are
// sorted by provider, then model. The top-level Entry with provider "*" holds
// grand totals.
func Aggregate(records []Record) (perModel []Entry, totals Entry) {
	bucket := make(map[string]*Entry)
	for _, r := range records {
		for _, e := range r.Entries {
			key := e.Provider + "|" + e.Model
			cur, ok := bucket[key]
			if !ok {
				cur = &Entry{Provider: e.Provider, Model: e.Model}
				bucket[key] = cur
			}
			cur.Calls += e.Calls
			cur.PromptTokens += e.PromptTokens
			cur.CompletionTokens += e.CompletionTokens
			cur.TotalTokens += e.TotalTokens
			cur.CostUSD += e.CostUSD
		}
	}
	for _, e := range bucket {
		totals.Calls += e.Calls
		totals.PromptTokens += e.PromptTokens
		totals.CompletionTokens += e.CompletionTokens
		totals.TotalTokens += e.TotalTokens
		totals.CostUSD += e.CostUSD
		perModel = append(perModel, *e)
	}
	totals.Provider = "*"
	totals.Model = "*"
	sort.Slice(perModel, func(i, j int) bool {
		if perModel[i].Provider != perModel[j].Provider {
			return perModel[i].Provider < perModel[j].Provider
		}
		return perModel[i].Model < perModel[j].Model
	})
	return perModel, totals
}
