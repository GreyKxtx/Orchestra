package guard

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
type DiagTracker struct {
	mu     sync.Mutex
	last   map[string]string // path → last fingerprint
	repeat map[string]int    // path → consecutive identical fingerprints
	hint   map[string]string // path → last LSP error hint (for task_result gate)
}

func NewDiagTracker() *DiagTracker {
	return &DiagTracker{
		last:   make(map[string]string),
		repeat: make(map[string]int),
		hint:   make(map[string]string),
	}
}

// Observe records the post-write/edit diagnostic fingerprint for path
// and returns the number of consecutive identical non-empty
// fingerprints (counting this one). 0 means "no prior attempt", 1
// means "this is the first time we've seen this fingerprint", 2+
// means "model ran write/edit on this file and the diagnostic state
// did not change".
//
// Empty fingerprint (no errors) resets the streak. When hint is
// non-empty it is stored for PathsWithErrors / ErrorHint.
func (t *DiagTracker) Observe(path, fingerprint, hint string) int {
	if t == nil || path == "" {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if fingerprint == "" {
		// Clean state: any past repeats become irrelevant.
		delete(t.last, path)
		delete(t.repeat, path)
		delete(t.hint, path)
		return 0
	}
	if hint != "" {
		t.hint[path] = hint
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

// PathsWithErrors returns sorted paths whose last write/edit still has
// LSP error-severity diagnostics (non-empty fingerprint).
func (t *DiagTracker) PathsWithErrors() []string {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.last) == 0 {
		return nil
	}
	out := make([]string, 0, len(t.last))
	for p, fp := range t.last {
		if fp != "" {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// ErrorHint returns the last stored LSP hint for path (may be "").
func (t *DiagTracker) ErrorHint(path string) string {
	if t == nil || path == "" {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.hint[path]
}

// fingerprintLSPErrors extracts a stable hash of error-severity
// diagnostics from a write/edit tool result. Returns "" when no errors
// (or unparseable input). The fingerprint covers (line, col, first 60
// chars of message) sorted lexically — enough that a cosmetic re-write
// that doesn't change the underlying problem produces an identical
// hash.
func FingerprintLSPErrors(out json.RawMessage) string {
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
func ExtractWriteOrEditPath(input json.RawMessage) string {
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
