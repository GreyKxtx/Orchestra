package lsp

import (
	"context"
	"encoding/json"
	"sync"
)

// DiagnosticsCache stores the latest diagnostics per URI and notifies waiters.
type DiagnosticsCache struct {
	mu      sync.Mutex
	entries map[string][]Diagnostic
	waiters []*diagWaiter
}

type diagWaiter struct {
	uri string
	ch  chan []Diagnostic
}

// NewDiagnosticsCache creates an empty cache.
func NewDiagnosticsCache() *DiagnosticsCache {
	return &DiagnosticsCache{entries: make(map[string][]Diagnostic)}
}

// Update stores new diagnostics for uri and wakes matching waiters.
func (dc *DiagnosticsCache) Update(uri string, diags []Diagnostic) {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	if diags == nil {
		diags = []Diagnostic{}
	}
	dc.entries[uri] = diags

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
	return d
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
func (dc *DiagnosticsCache) HandleNotification(params json.RawMessage) {
	var p struct {
		URI         string       `json:"uri"`
		Diagnostics []Diagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	dc.Update(p.URI, p.Diagnostics)
}
