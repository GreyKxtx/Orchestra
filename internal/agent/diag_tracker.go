package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// diagTracker remembers the LSP diagnostic fingerprint of the most
// recent write/edit attempt per file, so a model that "fixes" the file
// with cosmetic re-writes (whitespace change, comment move) is detected
// when the diagnostic output is identical to the previous attempt.
//
// H7 in architecture audit replaces the Sprint 6 prompt-hack
// (extractLSPErrors imperative message) with this structural check.
// The hack told the model "next identical write will be blocked";
// this actually detects when the model's edit didn't change the
// compile state, regardless of the input bytes.
//
// Tracking is in-memory, per-Agent. State is reset on Agent
// construction; long-running agents accumulate one entry per touched
// file (small).
type diagTracker struct {
	mu     sync.Mutex
	last   map[string]string // path → last fingerprint
	repeat map[string]int    // path → consecutive identical fingerprints
}

func newDiagTracker() *diagTracker {
	return &diagTracker{
		last:   make(map[string]string),
		repeat: make(map[string]int),
	}
}

// Observe records the post-write/edit diagnostic fingerprint for path
// and returns the number of consecutive identical non-empty
// fingerprints (counting this one). 0 means "no prior attempt", 1
// means "this is the first time we've seen this fingerprint", 2+
// means "model ran write/edit on this file and the diagnostic state
// did not change".
//
// Empty fingerprint (no errors) resets the streak.
func (t *diagTracker) Observe(path, fingerprint string) int {
	if t == nil || path == "" {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if fingerprint == "" {
		// Clean state: any past repeats become irrelevant.
		delete(t.last, path)
		delete(t.repeat, path)
		return 0
	}
	prev, had := t.last[path]
	t.last[path] = fingerprint
	if had && prev == fingerprint {
		t.repeat[path]++
		return t.repeat[path] + 1
	}
	t.repeat[path] = 0
	return 1
}

// fingerprintLSPErrors extracts a stable hash of error-severity
// diagnostics from a write/edit tool result. Returns "" when no errors
// (or unparseable input). The fingerprint covers (line, col, first 60
// chars of message) sorted lexically — enough that a cosmetic re-write
// that doesn't change the underlying problem produces an identical
// hash.
func fingerprintLSPErrors(out json.RawMessage) string {
	if len(out) == 0 {
		return ""
	}
	var resp struct {
		Diagnostics []struct {
			Severity  string `json:"severity"`
			Message   string `json:"message"`
			StartLine int    `json:"start_line"`
			StartCol  int    `json:"start_col"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return ""
	}
	keys := make([]string, 0, len(resp.Diagnostics))
	for _, d := range resp.Diagnostics {
		if d.Severity != "error" {
			continue
		}
		msg := d.Message
		if len(msg) > 60 {
			msg = msg[:60]
		}
		keys = append(keys, fmt.Sprintf("%d:%d:%s", d.StartLine, d.StartCol, msg))
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)
	h := sha256.Sum256([]byte(joinNewline(keys)))
	return hex.EncodeToString(h[:8])
}

func joinNewline(ss []string) string {
	n := 0
	for _, s := range ss {
		n += len(s) + 1
	}
	out := make([]byte, 0, n)
	for i, s := range ss {
		if i > 0 {
			out = append(out, '\n')
		}
		out = append(out, s...)
	}
	return string(out)
}

// extractWriteOrEditPath pulls the "path" field from a write/edit
// tool input. Returns "" when the input is malformed.
func extractWriteOrEditPath(input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &p); err != nil {
		return ""
	}
	return p.Path
}
