package lsp_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/lsp"
	"github.com/orchestra/orchestra/internal/lsp/lsptest"
)

func TestManager_IsEmpty_Disabled(t *testing.T) {
	disabled := false
	m, errs := lsp.NewManager("/workspace", lsp.LSPConfig{Enabled: &disabled})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if !m.IsEmpty() {
		t.Fatal("expected manager to be empty when disabled")
	}
	m.Close()
}

func TestManager_IsEmpty_NoServers(t *testing.T) {
	m, errs := lsp.NewManager("/workspace", lsp.LSPConfig{})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if !m.IsEmpty() {
		t.Fatal("expected empty manager with no servers")
	}
	m.Close()
}

func TestManager_IsEmpty_NilManager(t *testing.T) {
	var m *lsp.Manager
	if !m.IsEmpty() {
		t.Fatal("nil manager must report IsEmpty=true")
	}
}

func TestManager_StartErrors_BadCommand(t *testing.T) {
	enabled := true
	cfg := lsp.LSPConfig{
		Enabled: &enabled,
		Servers: []lsp.LSPServerConfig{
			{
				Language:   "go",
				Extensions: []string{".go"},
				Command:    []string{"__nonexistent_binary_xyz__"},
			},
		},
	}
	m, errs := lsp.NewManager("/workspace", cfg)
	if len(errs) == 0 {
		t.Fatal("expected start error for nonexistent binary")
	}
	if m == nil {
		t.Fatal("manager must not be nil even when start fails")
	}
	m.Close()
}

func TestManager_DisabledServer_Skipped(t *testing.T) {
	enabled := true
	cfg := lsp.LSPConfig{
		Enabled: &enabled,
		Servers: []lsp.LSPServerConfig{
			{
				Language:   "go",
				Extensions: []string{".go"},
				Command:    []string{"gopls"},
				Disabled:   true,
			},
		},
	}
	m, errs := lsp.NewManager("/workspace", cfg)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if !m.IsEmpty() {
		t.Fatal("disabled server should result in empty manager")
	}
	m.Close()
}

func TestManager_DocumentSymbols_HierarchicalFormat(t *testing.T) {
	// Create a temp workspace with a fake .go file.
	root := t.TempDir()
	content := "package main\n\nfunc Hello() {}\n"
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	conn, srv := lsptest.NewConn()
	// Respond with DocumentSymbol[] (hierarchical format — gopls style).
	srv.SetHandler("textDocument/documentSymbol", func(_ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`[
			{"name":"Hello","kind":12,"range":{"start":{"line":2,"character":0},"end":{"line":2,"character":16}},"selectionRange":{"start":{"line":2,"character":5},"end":{"line":2,"character":10}}}
		]`), nil
	})

	c, err := lsp.StartFromConn("test", conn, lsp.PathToURI(root), nil)
	if err != nil {
		t.Fatal(err)
	}
	m := lsp.ForTest(root, c, []string{".go"}, 1500)
	defer m.Close()

	syms, err := m.DocumentSymbols(context.Background(), "main.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(syms))
	}
	if syms[0].Name != "Hello" {
		t.Errorf("name: got %q, want %q", syms[0].Name, "Hello")
	}
	if syms[0].Kind != "function" {
		t.Errorf("kind: got %q, want %q", syms[0].Kind, "function")
	}
	// selectionRange line=2 → StartLine=3 (1-based).
	if syms[0].StartLine != 3 {
		t.Errorf("start_line: got %d, want 3", syms[0].StartLine)
	}
}

func TestManager_DocumentSymbols_FlatFormat(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	conn, srv := lsptest.NewConn()
	// Respond with SymbolInformation[] (flat/older format).
	rootURI := lsp.PathToURI(root)
	srv.SetHandler("textDocument/documentSymbol", func(_ json.RawMessage) (json.RawMessage, error) {
		resp := `[{"name":"MyFunc","kind":12,"location":{"uri":"` + rootURI + `/main.go","range":{"start":{"line":0,"character":0},"end":{"line":2,"character":1}}}}]`
		return json.RawMessage(resp), nil
	})

	c, err := lsp.StartFromConn("test", conn, rootURI, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := lsp.ForTest(root, c, []string{".go"}, 1500)
	defer m.Close()

	syms, err := m.DocumentSymbols(context.Background(), "main.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(syms))
	}
	if syms[0].Name != "MyFunc" {
		t.Errorf("name: got %q", syms[0].Name)
	}
}

func TestManager_DocumentSymbols_EmptyResponse(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	conn, srv := lsptest.NewConn()
	srv.SetHandler("textDocument/documentSymbol", func(_ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`null`), nil
	})

	c, err := lsp.StartFromConn("test", conn, lsp.PathToURI(root), nil)
	if err != nil {
		t.Fatal(err)
	}
	m := lsp.ForTest(root, c, []string{".go"}, 1500)
	defer m.Close()

	syms, err := m.DocumentSymbols(context.Background(), "main.go")
	if err != nil {
		t.Fatal(err)
	}
	if syms != nil {
		t.Fatalf("expected nil for null response, got %v", syms)
	}
}
