package lsp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/lsp/provision"
	"github.com/orchestra/orchestra/internal/lsp/registry"
)

type blockingInstall struct {
	release chan struct{}
	calls   atomic.Int32
}

func (f *blockingInstall) Install(ctx context.Context, e registry.Entry, destDir string) error {
	f.calls.Add(1)
	select {
	case <-f.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	bin := e.BinaryName
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	return os.WriteFile(filepath.Join(destDir, bin), []byte("x"), 0o755)
}

func TestManager_AsyncEnsure_PendingThenReady(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "c")
	t.Setenv("ORCHESTRA_LSP_CACHE", cache)
	block := &blockingInstall{release: make(chan struct{})}
	provision.SetInstallerForTest(block)
	t.Cleanup(func() {
		provision.SetInstallerForTest(nil)
		select {
		case <-block.release:
		default:
			close(block.release)
		}
	})

	lazy := true
	m, _ := NewManager(t.TempDir(), LSPConfig{
		LazyStart:   &lazy,
		AutoInstall: "true",
		Servers: []LSPServerConfig{{
			Language:   "go",
			Extensions: []string{".go"},
			Command:    []string{"gopls", "serve"},
		}},
	})
	t.Cleanup(m.Close)
	// 0 = return ErrEnsurePending immediately after starting background Ensure.
	m.SetEnsureSyncBudgetForTest(0)

	hookCalls := 0
	m.startServerHook = func(cfg LSPServerConfig, rootURI string) (*Client, error) {
		hookCalls++
		return nil, context.Canceled
	}

	_, err := m.Definition(context.Background(), "main.go", ToolPosition{Line: 1, Col: 1})
	if !errors.Is(err, provision.ErrEnsurePending) {
		t.Fatalf("expected ErrEnsurePending, got %v", err)
	}
	if hookCalls != 0 {
		t.Fatalf("hook should not run while ensure pending, calls=%d", hookCalls)
	}
	if m.RuntimeStatus() != "installing" {
		t.Fatalf("status=%q want installing", m.RuntimeStatus())
	}

	// Give the background Ensure a moment to enter Install (or finish cache-hit).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && block.calls.Load() == 0 && m.hasPendingEnsures() {
		time.Sleep(10 * time.Millisecond)
	}
	if block.calls.Load() == 0 && m.hasPendingEnsures() {
		// Still pending without Install → cache hit path finished Ensure already.
		// Re-check: pending should clear quickly on cache hit.
		time.Sleep(50 * time.Millisecond)
	}
	if !m.hasPendingEnsures() && block.calls.Load() == 0 {
		t.Fatal("expected either in-flight Install or still-pending job")
	}

	if p := m.GetInstallProgress(); p == nil {
		t.Fatal("expected install progress event")
	}

	close(block.release)
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !m.hasPendingEnsures() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("ensure still pending after release")
}
