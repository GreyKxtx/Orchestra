package provision_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/lsp/provision"
	"github.com/orchestra/orchestra/internal/lsp/registry"
)

type progressFakeInstall struct{ delay time.Duration }

func (f *progressFakeInstall) Install(ctx context.Context, e registry.Entry, destDir string) error {
	time.Sleep(f.delay)
	bin := e.BinaryName
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	return os.WriteFile(filepath.Join(destDir, bin), []byte("x"), 0o755)
}

func TestEnsure_ReportsProgress(t *testing.T) {
	t.Setenv("ORCHESTRA_LSP_CACHE", t.TempDir())
	provision.SetInstallerForTest(&progressFakeInstall{delay: 5 * time.Millisecond})
	t.Cleanup(func() { provision.SetInstallerForTest(nil) })

	var mu sync.Mutex
	var phases []string
	ctx := provision.WithProgress(context.Background(), func(ev provision.ProgressEvent) {
		mu.Lock()
		phases = append(phases, ev.Phase)
		mu.Unlock()
	})
	if err := provision.Ensure(ctx, "gopls"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	need := map[string]bool{"starting": false, "installing": false, "verifying": false, "done": false}
	for _, p := range phases {
		if _, ok := need[p]; ok {
			need[p] = true
		}
	}
	for p, ok := range need {
		if !ok {
			t.Fatalf("missing phase %q in %v", p, phases)
		}
	}
}
