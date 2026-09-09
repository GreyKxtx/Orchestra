package lsp

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

// newClosableManager builds a lazy Manager with one server whose command is an
// unknown binary, so startServer routes through startServerHook instead of the
// real resolver. lazyStart is then flipped off so WarmupStart has work to do.
func newClosableManager(t *testing.T) (*Manager, *atomic.Int32) {
	t.Helper()
	lazy := true
	m, errs := NewManager(t.TempDir(), LSPConfig{
		LazyStart:   &lazy,
		AutoInstall: "false",
		Servers: []LSPServerConfig{{
			Language:   "zz",
			Extensions: []string{".zz"},
			Command:    []string{"dummy-lsp-zz"},
		}},
	})
	if len(errs) > 0 {
		t.Fatalf("NewManager errs=%v", errs)
	}
	var spawned atomic.Int32
	m.startServerHook = func(cfg LSPServerConfig, rootURI string) (*Client, error) {
		spawned.Add(1)
		return nil, errors.New("hook: refuse to spawn")
	}
	m.lazyStart = false
	return m, &spawned
}

// A closed Manager is terminal: WarmupStart after Close must not try to spawn
// a server, otherwise a project closed while its warmup is still running leaks
// a language-server process.
func TestManager_WarmupStartAfterCloseDoesNotSpawn(t *testing.T) {
	m, spawned := newClosableManager(t)
	entry := m.servers[0] // captured before Close, as a lazy tool call would hold it
	m.Close()
	m.WarmupStart(context.Background())
	if err := m.ensureClient(entry); err == nil {
		t.Fatal("ensureClient after Close returned nil error; want a closed error")
	}
	if n := spawned.Load(); n != 0 {
		t.Fatalf("after Close, %d server spawn(s) were attempted; want 0", n)
	}
}

// Close racing with WarmupStart must be free of data races (run with -race).
// This is exactly what happens when a project is closed while the core is
// still warming its LSP servers.
func TestManager_CloseConcurrentWithWarmupStart(t *testing.T) {
	m, _ := newClosableManager(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.WarmupStart(context.Background())
		_ = m.IsEmpty()
		_ = m.RuntimeStatus()
	}()
	m.Close()
	<-done
	m.Close() // idempotent
}
