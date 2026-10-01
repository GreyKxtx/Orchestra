package tools

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/orchestra/orchestra/internal/lsp"
	"github.com/orchestra/orchestra/internal/permission"
)

type countingRequester struct {
	mu    sync.Mutex
	calls int
	resp  permission.Response
}

func (c *countingRequester) RequestPermission(context.Context, permission.Request) (permission.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return c.resp, nil
}

func newAskRunner(t *testing.T) *Runner {
	t.Helper()
	root := t.TempDir()
	// A real YAML file: the only server detected is yaml-language-server,
	// which is missing on a clean PATH.
	if err := os.WriteFile(filepath.Join(root, "compose.yml"), []byte("x: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	t.Setenv("ORCHESTRA_LSP_CACHE", t.TempDir())
	r, err := NewRunner(root, RunnerOptions{DryRun: true})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	lazy := true
	m, errs := lsp.NewManager(root, lsp.LSPConfig{
		LazyStart:   &lazy,
		AutoInstall: "ask",
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
	r.lspAutoInstall = "ask"
	return r
}

// Every turn runs WarmupLSP. A "Skip" that was not remembered put the same
// prompt in front of the user on every message.
func TestWarmupLSP_SkipIsRemembered(t *testing.T) {
	r := newAskRunner(t)
	req := &countingRequester{resp: permission.Response{Approved: false}}
	ctx := permission.WithRequester(context.Background(), req)

	r.WarmupLSP(ctx)
	r.WarmupLSP(ctx)
	r.WarmupLSP(ctx)

	if req.calls != 1 {
		t.Fatalf("declined install asked %d times, want once", req.calls)
	}
}

func TestLSPConsentMemory_AlwaysSwitchesToSilent(t *testing.T) {
	var m lspConsentMemory
	m.record(permission.Request{Reason: "yaml-language-server"}, permission.Response{Approved: true, Always: true})
	if !m.always {
		t.Fatal("Always must switch the runner to silent installs")
	}
}

func TestLSPConsentMemory_NewLanguageAsksAgain(t *testing.T) {
	var m lspConsentMemory
	m.record(permission.Request{Reason: "yaml-language-server"}, permission.Response{Approved: false})
	if !m.declinedAll([]string{"yaml-language-server"}) {
		t.Fatal("a declined server must not be asked for again")
	}
	if m.declinedAll([]string{"yaml-language-server", "pyright"}) {
		t.Fatal("a newly detected language must be asked for")
	}
}
