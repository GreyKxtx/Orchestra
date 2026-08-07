package provision_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/lsp/provision"
	"github.com/orchestra/orchestra/internal/lsp/registry"
)

func TestCacheBinaryPath_Layout(t *testing.T) {
	t.Setenv("ORCHESTRA_LSP_CACHE", filepath.Join(t.TempDir(), "lsp-cache"))
	p, err := provision.CacheBinaryPath("gopls", "latest", "gopls")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(filepath.Dir(p)) != "latest" {
		t.Fatalf("version dir: %s", p)
	}
	if filepath.Base(filepath.Dir(filepath.Dir(p))) != "gopls" {
		t.Fatalf("id dir: %s", p)
	}
	if runtime.GOOS == "windows" {
		if filepath.Ext(p) != ".exe" {
			t.Fatalf("want .exe on windows, got %s", p)
		}
	}
}

func TestResolve_MissingKnownBinary(t *testing.T) {
	t.Setenv("ORCHESTRA_LSP_CACHE", filepath.Join(t.TempDir(), "empty-cache"))
	_, err := provision.Resolve([]string{"basedpyright-langserver", "--stdio"})
	if err == nil {
		t.Skip("basedpyright-langserver unexpectedly on PATH")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(err.Error(), "orchestra lsp ensure") {
		t.Fatalf("expected ensure hint, got %v", err)
	}
}

func TestResolve_CacheHit(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "cache")
	t.Setenv("ORCHESTRA_LSP_CACHE", cache)
	e, ok := registry.ByID("gopls")
	if !ok {
		t.Fatal("gopls missing from registry")
	}
	p, err := provision.CacheBinaryPath(e.ID, e.Version, e.BinaryName)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := provision.Resolve([]string{"gopls", "serve"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Source != provision.SourceCache {
		t.Fatalf("source=%s want cache", res.Source)
	}
	if res.Command[0] != p {
		t.Fatalf("cmd[0]=%s want %s", res.Command[0], p)
	}
}

func TestResolve_Absolute(t *testing.T) {
	dir := t.TempDir()
	name := "custom-ls"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := provision.Resolve([]string{p, "--stdio"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Source != provision.SourceAbsolute {
		t.Fatalf("source=%s", res.Source)
	}
}

func TestInspectConfigured(t *testing.T) {
	t.Setenv("ORCHESTRA_LSP_CACHE", filepath.Join(t.TempDir(), "c"))
	st := provision.InspectConfigured([]provision.ConfiguredServer{
		{Language: "go", Extensions: []string{".go"}, Command: []string{"gopls", "serve"}},
	})
	if len(st) != 1 {
		t.Fatalf("len=%d", len(st))
	}
	if st[0].Language != "go" {
		t.Fatalf("%+v", st[0])
	}
}

func TestRegistry_ByExtension(t *testing.T) {
	e, ok := registry.ByExtension(".ts")
	if !ok || e.ID != "typescript-language-server" {
		t.Fatalf("got %+v ok=%v", e, ok)
	}
}
