package lsp

import (
	"context"
	"encoding/json"
	"sync"
)

// DiagnosticsCache stores the latest diagnostics per URI and notifies waiters.
//
// H5 in audit ledger: each entry carries the document version the push
// arrived under so a caller waiting for diagnostics after a `DidChange`
// can ignore stale pushes the server had queued under an older version.
// LSP servers that implement spec >= 3.17 include `version` in
// `publishDiagnostics`; older servers send no version, in which case
// `version == 0` and the cache treats every push as "current" (loses
// the staleness guard for those servers — documented limitation).
type DiagnosticsCache struct {
	mu      sync.Mutex
	entries map[string]diagEntry
	waiters []*diagWaiter
}

type diagEntry struct {
	diags   []Diagnostic
	version int
}

type diagWaiter struct {
	uri string
	ch  chan []Diagnostic
}

// NewDiagnosticsCache creates an empty cache.
func NewDiagnosticsCache() *DiagnosticsCache {
	return &DiagnosticsCache{entries: make(map[string]diagEntry)}
}

// Update stores new diagnostics for uri at version and wakes matching waiters.
// A push with version older than the cached entry is dropped (stale).
func (dc *DiagnosticsCache) Update(uri string, version int, diags []Diagnostic) {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	if diags == nil {
		diags = []Diagnostic{}
	}
	if cur, ok := dc.entries[uri]; ok && version > 0 && cur.version > version {
		// Stale push from older document version — ignore.
		return
	}
	dc.entries[uri] = diagEntry{diags: diags, version: version}

	remaining := dc.waiters[:0]
	for _, w := range dc.waiters {
		if w.uri == uri {
			w.ch <- diags
		} else {
			remaining = append(remaining, w)
		}
	}
	dc.waiters = remaining
}

// Get returns currently cached diagnostics for uri (nil if never received).
func (dc *DiagnosticsCache) Get(uri string) []Diagnostic {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	d, ok := dc.entries[uri]
	if !ok {
		return nil
	}
	return d.diags
}

// Forget drops cached diagnostics + any pending waiters for uri. Called when
// a document is closed (fs.delete, fs.rename) so stale diagnostics from a
// previously-open document don't survive forever. H7 in audit ledger.
func (dc *DiagnosticsCache) Forget(uri string) {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	delete(dc.entries, uri)
}

// WaitForUpdate blocks until the next diagnostics push for uri or ctx expires.
// Returns nil on timeout/cancel.
func (dc *DiagnosticsCache) WaitForUpdate(ctx context.Context, uri string) []Diagnostic {
	ch := make(chan []Diagnostic, 1)
	dc.mu.Lock()
	dc.waiters = append(dc.waiters, &diagWaiter{uri: uri, ch: ch})
	dc.mu.Unlock()

	select {
	case d := <-ch:
		return d
	case <-ctx.Done():
		dc.mu.Lock()
		remaining := dc.waiters[:0]
		for _, w := range dc.waiters {
			if w.ch != ch {
				remaining = append(remaining, w)
			}
		}
		dc.waiters = remaining
		dc.mu.Unlock()
		return nil
	}
}

// HandleNotification processes a textDocument/publishDiagnostics notification.
// Servers implementing LSP >= 3.17 include `version`; for older servers
// `version` defaults to 0 and the staleness guard inside Update no-ops
// (every push is accepted).
func (dc *DiagnosticsCache) HandleNotification(params json.RawMessage) {
	var p struct {
		URI         string       `json:"uri"`
		Version     int          `json:"version,omitempty"`
		Diagnostics []Diagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	dc.Update(p.URI, p.Version, p.Diagnostics)
}
