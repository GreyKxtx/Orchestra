package provision_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/orchestra/orchestra/internal/lsp/provision"
	"github.com/orchestra/orchestra/internal/lsp/registry"
)

type fakeInstaller struct {
	called int
	err    error
}

func (f *fakeInstaller) Install(ctx context.Context, e registry.Entry, destDir string) error {
	f.called++
	if f.err != nil {
		return f.err
	}
	bin := e.BinaryName
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	p := filepath.Join(destDir, bin)
	return os.WriteFile(p, []byte("fake-gopls"), 0o755)
}

func TestEnsure_Gopls_FakeInstaller(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "cache")
	t.Setenv("ORCHESTRA_LSP_CACHE", cache)
	fake := &fakeInstaller{}
	provision.SetInstallerForTest(fake)
	t.Cleanup(func() { provision.SetInstallerForTest(nil) })

	if err := provision.Ensure(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	if fake.called != 1 {
		t.Fatalf("called=%d", fake.called)
	}
	// Second call is no-op when binary exists.
	if err := provision.Ensure(context.Background(), "gopls"); err != nil {
		t.Fatal(err)
	}
	if fake.called != 1 {
		t.Fatalf("expected cache hit, called=%d", fake.called)
	}
	res, err := provision.Resolve([]string{"gopls", "serve"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Source != provision.SourceCache {
		t.Fatalf("source=%s", res.Source)
	}
}

func TestEnsure_Unsupported(t *testing.T) {
	err := provision.Ensure(context.Background(), "rust-analyzer")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestUpgrade_Reinstalls(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "cache")
	t.Setenv("ORCHESTRA_LSP_CACHE", cache)
	fake := &fakeInstaller{}
	provision.SetInstallerForTest(fake)
	t.Cleanup(func() { provision.SetInstallerForTest(nil) })

	if err := provision.Ensure(context.Background(), "gopls"); err != nil {
		t.Fatal(err)
	}
	if fake.called != 1 {
		t.Fatalf("ensure called=%d", fake.called)
	}
	if err := provision.Upgrade(context.Background(), "gopls"); err != nil {
		t.Fatal(err)
	}
	if fake.called != 2 {
		t.Fatalf("upgrade should reinstall, called=%d", fake.called)
	}
}
