package tools

import (
	"context"
	"testing"

	"github.com/orchestra/orchestra/internal/lsp"
)

// Runner.Close racing with a background WarmupLSP must be free of data races
// (run with -race). core.Core.WarmupLSP launches Runner.WarmupLSP in a
// goroutine; when several cores live in one process a project can be closed
// while its warmup is still in flight.
func TestRunner_CloseConcurrentWithWarmupLSP(t *testing.T) {
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{DryRun: true})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	lazy := true
	m, errs := lsp.NewManager(root, lsp.LSPConfig{
		LazyStart:   &lazy,
		AutoInstall: "false",
		Servers: []lsp.LSPServerConfig{{
			Language:   "zz",
			Extensions: []string{".zz"},
			Command:    []string{"dummy-lsp-zz"},
		}},
	})
	if len(errs) > 0 {
		t.Fatalf("NewManager errs=%v", errs)
	}
	r.lspManager = m
	r.lspAutoInstall = "false"

	done := make(chan struct{})
	go func() {
		defer close(done)
		r.WarmupLSP(context.Background())
	}()
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	<-done

	// Status queries after Close must stay safe: the manager is inert, not gone.
	_ = r.LSPStatus()
	if err := r.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
