package lsp

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
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
	m.SetInstallConsent(consent)
	m.SetEnsureSyncBudgetForTest(-1) // always sync for this test

	m.startServerHook = func(cfg LSPServerConfig, rootURI string) (*Client, error) {
		if len(cfg.Command) == 0 || !filepath.IsAbs(cfg.Command[0]) {
			t.Fatalf("expected abs cache path, got %v", cfg.Command)
		}
		return nil, context.Canceled
	}

	_, err := m.Definition(context.Background(), "main.go", ToolPosition{Line: 1, Col: 1})
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
	m.SetInstallConsent(denyAll{})
	_, err := m.Definition(context.Background(), "main.go", ToolPosition{Line: 1, Col: 1})
	if err == nil {
		t.Fatal("expected deny error")
	}
}
