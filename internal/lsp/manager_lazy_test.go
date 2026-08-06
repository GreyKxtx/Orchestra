package lsp_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/lsp"
	"github.com/orchestra/orchestra/internal/lsp/lsptest"
)

type mockStagedProvider struct {
	paths   []string
	content map[string]string
}

func (p *mockStagedProvider) EffectiveContent(relPath string) (string, bool) {
	c, ok := p.content[relPath]
	return c, ok
}

func (p *mockStagedProvider) ListStagedPaths() []string {
	return append([]string(nil), p.paths...)
}

func TestManager_LazyStart_NoClientUntilFirstUse(t *testing.T) {
	enabled := true
	m, errs := lsp.NewManager("/workspace", lsp.LSPConfig{
		Enabled: &enabled,
		Servers: []lsp.LSPServerConfig{{
			Language:   "go",
			Extensions: []string{".go"},
			Command:    []string{"gopls"},
		}},
	})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	defer m.Close()
	if m.IsEmpty() {
		t.Fatal("expected configured servers")
	}
	if m.ClientRunningForTest("go") {
		t.Fatal("lazy manager must not spawn client at init")
	}
}

func TestManager_LazyStart_StartsOnFirstUse(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	enabled := true
	m, errs := lsp.NewManager(root, lsp.LSPConfig{Enabled: &enabled, Servers: []lsp.LSPServerConfig{{
		Language:   "go",
		Extensions: []string{".go"},
		Command:    []string{"unused"},
	}}})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	defer m.Close()

	m.SetStartServerHookForTest(func(_ lsp.LSPServerConfig, rootURI string) (*lsp.Client, error) {
		conn, srv := lsptest.NewConn()
		srv.SetHandler("textDocument/documentSymbol", func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`[]`), nil
		})
		return lsp.StartFromConn("test", conn, rootURI, nil)
	})

	if _, err := m.DocumentSymbols(context.Background(), "main.go"); err != nil {
		t.Fatalf("DocumentSymbols: %v", err)
	}
	if !m.ClientRunningForTest("go") {
		t.Fatal("expected client running after first use")
	}
}

func TestManager_LazyStart_StartErrorDeferred(t *testing.T) {
	enabled := true
	m, errs := lsp.NewManager("/workspace", lsp.LSPConfig{Enabled: &enabled, Servers: []lsp.LSPServerConfig{{
		Language:   "go",
		Extensions: []string{".go"},
		Command:    []string{"__nonexistent_binary_xyz__"},
	}}})
	if len(errs) != 0 {
		t.Fatalf("lazy init must not return start errors, got %v", errs)
	}
	defer m.Close()
	if m.IsEmpty() {
		t.Fatal("server should be configured")
	}
	_, err := m.DocumentSymbols(context.Background(), "main.go")
	if err == nil {
		t.Fatal("expected error on first use with bad command")
	}
}

func TestManager_TTL_ShutdownAndReopenStaged(t *testing.T) {
	root := t.TempDir()
	disk := "package main\n\nfunc Disk() {}\n"
	staged := "package main\n\nfunc Staged() {}\n"
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(disk), 0644); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var opens []string
	registerMock := func(srv *lsptest.Server) {
		srv.SetHandler("textDocument/didOpen", func(params json.RawMessage) (json.RawMessage, error) {
			var p struct {
				TextDocument struct {
					Text string `json:"text"`
				} `json:"textDocument"`
			}
			_ = json.Unmarshal(params, &p)
			mu.Lock()
			opens = append(opens, p.TextDocument.Text)
			mu.Unlock()
			return json.RawMessage(`null`), nil
		})
		srv.SetHandler("textDocument/documentSymbol", func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`[]`), nil
		})
	}

	ttl := 1
	enabled := true
	m, errs := lsp.NewManager(root, lsp.LSPConfig{
		Enabled:        &enabled,
		IdleTTLSeconds: &ttl,
		Servers: []lsp.LSPServerConfig{{
			Language:   "go",
			Extensions: []string{".go"},
			Command:    []string{"unused"},
		}},
	})
	if len(errs) != 0 {
		t.Fatalf("NewManager: %v", errs)
	}
	defer m.Close()

	startCount := 0
	m.SetStartServerHookForTest(func(_ lsp.LSPServerConfig, rootURI string) (*lsp.Client, error) {
		startCount++
		conn, srv := lsptest.NewConn()
		registerMock(srv)
		return lsp.StartFromConn("test", conn, rootURI, nil)
	})

	m.SetContentProvider(&mockStagedProvider{
		paths:   []string{"main.go"},
		content: map[string]string{"main.go": staged},
	})

	if _, err := m.DocumentSymbols(context.Background(), "main.go"); err != nil {
		t.Fatalf("first use: %v", err)
	}
	if !m.ClientRunningForTest("go") {
		t.Fatal("client should be running")
	}

	m.SetLastActivityForTest("go", time.Now().Add(-2*time.Second))
	m.CheckIdleShutdownForTest()
	if m.ClientRunningForTest("go") {
		t.Fatal("client should be shut down after TTL")
	}

	if _, err := m.DocumentSymbols(context.Background(), "main.go"); err != nil {
		t.Fatalf("second use after TTL: %v", err)
	}
	if startCount < 2 {
		t.Fatalf("expected server restart after TTL, startCount=%d", startCount)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(opens)
		mu.Unlock()
		if n >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for staged reopen didOpen, opens=%d", n)
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	last := opens[len(opens)-1]
	mu.Unlock()
	if last != staged {
		t.Fatalf("reopen after wake: got %q, want staged %q", last, staged)
	}
}
