package lsp

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/lsp/provision"
	"github.com/orchestra/orchestra/internal/lsp/registry"
	"github.com/orchestra/orchestra/internal/permission"
)

type approveOnce struct {
	always bool
	n      int
}

func (a *approveOnce) RequestPermission(ctx context.Context, req permission.Request) (permission.Response, error) {
	a.n++
	return permission.Response{Approved: true, Always: a.always}, nil
}

type denyAll struct{}

func (denyAll) RequestPermission(ctx context.Context, req permission.Request) (permission.Response, error) {
	return permission.Response{Approved: false}, nil
}

type fakeInstall struct{ n int }

func (f *fakeInstall) Install(ctx context.Context, e registry.Entry, destDir string) error {
	f.n++
	bin := e.BinaryName
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	return os.WriteFile(filepath.Join(destDir, bin), []byte("x"), 0o755)
}

func TestManager_EnsureOnAsk_Approved(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "c")
	t.Setenv("ORCHESTRA_LSP_CACHE", cache)
	fake := &fakeInstall{}
	provision.SetInstallerForTest(fake)
	t.Cleanup(func() { provision.SetInstallerForTest(nil) })

	lazy := true
	m, errs := NewManager(t.TempDir(), LSPConfig{
		LazyStart:   &lazy,
		AutoInstall: "ask",
		Servers: []LSPServerConfig{{
			Language:   "go",
			Extensions: []string{".go"},
			Command:    []string{"gopls", "serve"},
		}},
	})
	if len(errs) > 0 {
		t.Fatalf("errs=%v", errs)
	}
	t.Cleanup(m.Close)
	consent := &approveOnce{}
	m.SetEnsureSyncBudgetForTest(-1) // always sync for this test

	m.startServerHook = func(cfg LSPServerConfig, rootURI string) (*Client, error) {
		if len(cfg.Command) == 0 || !filepath.IsAbs(cfg.Command[0]) {
			t.Fatalf("expected abs cache path, got %v", cfg.Command)
		}
		return nil, context.Canceled
	}

	_, err := m.Definition(permission.WithRequester(context.Background(), consent), "main.go", ToolPosition{Line: 1, Col: 1})
	if err == nil {
		t.Fatal("expected error from hook cancel")
	}
	if consent.n != 1 {
		t.Fatalf("consent calls=%d", consent.n)
	}
	if fake.n != 1 {
		t.Fatalf("install calls=%d", fake.n)
	}
}

func TestManager_EnsureOnAsk_Denied(t *testing.T) {
	t.Setenv("ORCHESTRA_LSP_CACHE", filepath.Join(t.TempDir(), "c"))
	lazy := true
	m, _ := NewManager(t.TempDir(), LSPConfig{
		LazyStart:   &lazy,
		AutoInstall: "ask",
		Servers: []LSPServerConfig{{
			Language:   "go",
			Extensions: []string{".go"},
			Command:    []string{"gopls", "serve"},
		}},
	})
	t.Cleanup(m.Close)
	_, err := m.Definition(permission.WithRequester(context.Background(), denyAll{}), "main.go", ToolPosition{Line: 1, Col: 1})
	if err == nil {
		t.Fatal("expected deny error")
	}
}

// The consent is the caller's, not the manager's (ARCH-5): a turn that
// declines does not answer for the next one, and a turn whose client
// approves gets the install even though the last one said no.
func TestManager_EnsureOnAsk_ConsentIsTheCallers(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "c")
	t.Setenv("ORCHESTRA_LSP_CACHE", cache)
	fake := &fakeInstall{}
	provision.SetInstallerForTest(fake)
	t.Cleanup(func() { provision.SetInstallerForTest(nil) })

	lazy := true
	m, errs := NewManager(t.TempDir(), LSPConfig{
		LazyStart:   &lazy,
		AutoInstall: "ask",
		Servers: []LSPServerConfig{{
			Language:   "go",
			Extensions: []string{".go"},
			Command:    []string{"gopls", "serve"},
		}},
	})
	if len(errs) > 0 {
		t.Fatalf("errs=%v", errs)
	}
	t.Cleanup(m.Close)
	m.SetEnsureSyncBudgetForTest(-1)
	m.startServerHook = func(cfg LSPServerConfig, rootURI string) (*Client, error) {
		return nil, context.Canceled
	}

	// A turn whose client declines.
	_, err := m.Definition(permission.WithRequester(context.Background(), denyAll{}), "main.go", ToolPosition{Line: 1, Col: 1})
	if err == nil || !strings.Contains(err.Error(), "declined") {
		t.Fatalf("a declined install: err = %v", err)
	}
	if fake.n != 0 {
		t.Fatalf("install calls after a decline = %d", fake.n)
	}
	// A call with no client of its own is not answered by the last one's:
	// nothing is installed and the error says there was nobody to ask.
	_, err = m.Definition(context.Background(), "main.go", ToolPosition{Line: 1, Col: 1})
	if err == nil || !strings.Contains(err.Error(), "no interactive consent") {
		t.Fatalf("a call with no client: err = %v, want 'no interactive consent'", err)
	}
	if fake.n != 0 {
		t.Fatalf("install calls without consent = %d", fake.n)
	}
	// The next turn's client approves: its answer, not the last one's, counts.
	consent := &approveOnce{}
	_, _ = m.Definition(permission.WithRequester(context.Background(), consent), "main.go", ToolPosition{Line: 1, Col: 1})
	if consent.n != 1 || fake.n != 1 {
		t.Fatalf("consent calls=%d install calls=%d, want 1 and 1", consent.n, fake.n)
	}
}
