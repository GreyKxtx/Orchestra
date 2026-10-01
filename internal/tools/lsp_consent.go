package tools

import (
	"context"
	"strings"
	"sync"

	"github.com/orchestra/orchestra/internal/permission"
)

// lspConsentMemory keeps the user's answer to the batch lsp.install prompt for
// the life of the runner. Every turn runs WarmupLSP, and a prompt that forgot
// "Skip" or "Always" came back in front of the user on every message.
type lspConsentMemory struct {
	// asking is held while one warmup is prompting or installing; a warmup
	// that finds it taken skips rather than stacking a second prompt.
	asking sync.Mutex

	mu       sync.Mutex
	always   bool
	declined map[string]bool
}

// record stores the answer to req. The prompt names its servers in Reason,
// comma-separated (provision.EnsureDetected).
func (m *lspConsentMemory) record(req permission.Request, resp permission.Response) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if resp.Approved {
		if resp.Always {
			m.always = true
		}
		return
	}
	if m.declined == nil {
		m.declined = map[string]bool{}
	}
	for _, id := range strings.Split(req.Reason, ",") {
		if id = strings.TrimSpace(id); id != "" {
			m.declined[id] = true
		}
	}
}

// silent reports that the user chose "Always".
func (m *lspConsentMemory) silent() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.always
}

// declinedAll reports that every id was already declined, so asking again
// would only repeat a question the user answered. A newly detected language
// is not in the set and is asked for.
func (m *lspConsentMemory) declinedAll(ids []string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range ids {
		if !m.declined[id] {
			return false
		}
	}
	return true
}

// recordingRequester passes a prompt through to the turn's client and keeps
// the answer in memory.
type recordingRequester struct {
	inner  permission.Requester
	memory *lspConsentMemory
}

func (r recordingRequester) RequestPermission(ctx context.Context, req permission.Request) (permission.Response, error) {
	resp, err := r.inner.RequestPermission(ctx, req)
	if err == nil {
		r.memory.record(req, resp)
	}
	return resp, err
}
