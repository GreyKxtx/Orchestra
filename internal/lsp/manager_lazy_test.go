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

func TestManager_WarmupStart_SpawnsClient(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	lazyOff := false
	enabled := true
	m, errs := lsp.NewManager(root, lsp.LSPConfig{
		Enabled:     &enabled,
		AutoInstall: "false",
		Servers: []lsp.LSPServerConfig{{
			Language:   "go",
			Extensions: []string{".go"},
			Command:    []string{"unused"},
		}},
	})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	defer m.Close()

	m.SetStartServerHookForTest(func(_ lsp.LSPServerConfig, rootURI string) (*lsp.Client, error) {
		conn, srv := lsptest.NewConn()
		srv.SetHandler("initialize", func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"capabilities":{}}`), nil
		})
		return lsp.StartFromConn("test", conn, rootURI, nil)
	})
	m.SetLazyStartForTest(lazyOff)

	if m.RuntimeStatus() != "idle" {
		t.Fatalf("before warmup want idle, got %s", m.RuntimeStatus())
	}
	m.WarmupStart(context.Background())
	if m.RuntimeStatus() != "active" {
		t.Fatalf("after warmup want active, got %s", m.RuntimeStatus())
	}
	if !m.ClientRunningForTest("go") {
		t.Fatal("expected client running after WarmupStart")
	}
}

func TestManager_WarmupStart_LazySkipsSpawn(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	enabled := true
	m, errs := lsp.NewManager(root, lsp.LSPConfig{
		Enabled:     &enabled,
		AutoInstall: "false",
		Servers: []lsp.LSPServerConfig{{
		Language:   "go",
		Extensions: []string{".go"},
		Command:    []string{"unused"},
	}}})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	defer m.Close()

	m.SetStartServerHookForTest(func(_ lsp.LSPServerConfig, rootURI string) (*lsp.Client, error) {
		conn, _ := lsptest.NewConn()
		return lsp.StartFromConn("test", conn, rootURI, nil)
	})

	m.WarmupStart(context.Background())
	if m.ClientRunningForTest("go") {
		t.Fatal("lazy WarmupStart must not spawn client")
	}
	if m.RuntimeStatus() != "idle" {
		t.Fatalf("want idle, got %s", m.RuntimeStatus())
	}
}

func TestManager_GoTSMonorepo_LazyPerExtension(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.ts"), []byte("export {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	starts := map[string]int{}
	enabled := true
	m, errs := lsp.NewManager(root, lsp.LSPConfig{
		Enabled:     &enabled,
		AutoInstall: "false",
		Servers: []lsp.LSPServerConfig{
		{Language: "go", Extensions: []string{".go"}, Command: []string{"unused-go"}},
		{Language: "typescript", Extensions: []string{".ts", ".tsx"}, Command: []string{"unused-ts"}},
	}})
	if len(errs) != 0 {
		t.Fatalf("NewManager: %v", errs)
	}
	defer m.Close()

	m.SetStartServerHookForTest(func(cfg lsp.LSPServerConfig, rootURI string) (*lsp.Client, error) {
		starts[cfg.Language]++
		conn, srv := lsptest.NewConn()
		srv.SetHandler("textDocument/documentSymbol", func(_ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`[]`), nil
		})
		return lsp.StartFromConn("test", conn, rootURI, nil)
	})

	if _, err := m.DocumentSymbols(context.Background(), "main.go"); err != nil {
		t.Fatalf("go touch: %v", err)
	}
	if !m.ClientRunningForTest("go") || m.ClientRunningForTest("typescript") {
		t.Fatal("only go server should run after .go touch")
	}
	if starts["go"] != 1 || starts["typescript"] != 0 {
		t.Fatalf("starts after go: %+v", starts)
	}

	if _, err := m.DocumentSymbols(context.Background(), "app.ts"); err != nil {
		t.Fatalf("ts touch: %v", err)
	}
	if !m.ClientRunningForTest("typescript") {
		t.Fatal("typescript server should run after .ts touch")
	}
	if starts["typescript"] != 1 {
		t.Fatalf("starts after ts: %+v", starts)
	}
}

func TestManager_LazyStart_StartsOnFirstUse(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	enabled := true
	m, errs := lsp.NewManager(root, lsp.LSPConfig{
		Enabled:     &enabled,
		AutoInstall: "false",
		Servers: []lsp.LSPServerConfig{{
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
		AutoInstall:    "false",
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

func TestManager_CrashRestartReopensStaged(t *testing.T) {
	root := t.TempDir()
	staged := "package main\n\nfunc AfterCrash() {}\n"
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var opens []string
	var liveClients []*lsp.Client
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

	enabled := true
	m, errs := lsp.NewManager(root, lsp.LSPConfig{
		Enabled:     &enabled,
		AutoInstall: "false",
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
		c, err := lsp.StartFromConn("test", conn, rootURI, nil)
		if err != nil {
			return nil, err
		}
		liveClients = append(liveClients, c)
		return c, nil
	})

	m.SetContentProvider(&mockStagedProvider{
		paths:   []string{"main.go"},
		content: map[string]string{"main.go": staged},
	})

	if _, err := m.DocumentSymbols(context.Background(), "main.go"); err != nil {
		t.Fatalf("first use: %v", err)
	}
	if startCount != 1 || len(liveClients) != 1 {
		t.Fatalf("startCount=%d liveClients=%d", startCount, len(liveClients))
	}

	_ = liveClients[0].Close()
	deadline := time.Now().Add(time.Second)
	for liveClients[0].IsDead() == false && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !liveClients[0].IsDead() {
		t.Fatal("client should be dead after Close")
	}

	if _, err := m.DocumentSymbols(context.Background(), "main.go"); err != nil {
		t.Fatalf("after crash: %v", err)
	}
	if startCount < 2 {
		t.Fatalf("expected restart after crash, startCount=%d", startCount)
	}

	deadline = time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(opens)
		mu.Unlock()
		if n >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for staged reopen after crash, opens=%d", n)
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	last := opens[len(opens)-1]
	mu.Unlock()
	if last != staged {
		t.Fatalf("reopen after crash: got %q, want staged %q", last, staged)
	}
}
