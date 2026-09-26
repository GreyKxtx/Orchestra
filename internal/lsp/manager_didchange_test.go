package lsp_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/lsp"
	"github.com/orchestra/orchestra/internal/lsp/lsptest"
)

// A document the server was given once stayed that version for the life of
// the server: an edit on disk, or a staged edit that reached the overlay
// without a sync, left the server answering about a text that was gone.
// Every request now sends the document again when it changed under the
// server, and only then (audit phase 7).
func TestManager_ADocumentChangedUnderTheServerIsSentAgain(t *testing.T) {
	root := t.TempDir()
	const v1 = "package main\n\nfunc One() {}\n"
	const v2 = "package main\n\nfunc Two() {}\n"
	const staged = "package main\n\nfunc Staged() {}\n"
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}
	conn, srv := lsptest.NewConn()
	opened := make(chan string, 4)
	changed := make(chan string, 4)
	text := func(params json.RawMessage) string {
		var p struct {
			TextDocument   struct{ Text string }   `json:"textDocument"`
			ContentChanges []struct{ Text string } `json:"contentChanges"`
		}
		_ = json.Unmarshal(params, &p)
		if len(p.ContentChanges) > 0 {
			return p.ContentChanges[0].Text
		}
		return p.TextDocument.Text
	}
	srv.SetHandler("textDocument/didOpen", func(params json.RawMessage) (json.RawMessage, error) {
		opened <- text(params)
		return json.RawMessage(`null`), nil
	})
	srv.SetHandler("textDocument/didChange", func(params json.RawMessage) (json.RawMessage, error) {
		changed <- text(params)
		return json.RawMessage(`null`), nil
	})
	srv.SetHandler("textDocument/documentSymbol", func(json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`[]`), nil
	})
	c, err := lsp.StartFromConn("test", conn, lsp.PathToURI(root), nil)
	if err != nil {
		t.Fatal(err)
	}
	m := lsp.ForTest(root, c, []string{".go"}, 1500)
	t.Cleanup(m.Close)
	ctx := context.Background()
	ask := func() {
		t.Helper()
		if _, err := m.DocumentSymbols(ctx, "main.go"); err != nil {
			t.Fatal(err)
		}
	}
	expect := func(ch <-chan string, want, what string) {
		t.Helper()
		select {
		case got := <-ch:
			if got != want {
				t.Fatalf("%s: sent %q, want %q", what, got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s: nothing was sent", what)
		}
	}
	quiet := func(what string) {
		t.Helper()
		select {
		case got := <-changed:
			t.Fatalf("%s: sent again for nothing: %q", what, got)
		case <-time.After(200 * time.Millisecond):
		}
	}

	ask()
	expect(opened, v1, "the first request opens the disk's version")
	ask()
	quiet("an unchanged document")

	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(v2), 0o644); err != nil {
		t.Fatal(err)
	}
	ask()
	expect(changed, v2, "a disk edit under the open document")
	ask()
	quiet("the same disk version")

	// A staged version the overlay holds without a sync is sent the same way.
	m.SetContentProvider(lsp.ContentProviderFunc(func(rel string) (string, bool) {
		if rel == "main.go" {
			return staged, true
		}
		return "", false
	}))
	ask()
	expect(changed, staged, "a staged edit under the open document")
	ask()
	quiet("the same staged version")
	if c.DocVersion(lsp.PathToURI(filepath.Join(root, "main.go"))) != 3 {
		t.Fatalf("one didOpen and two didChange: version %d", c.DocVersion(lsp.PathToURI(filepath.Join(root, "main.go"))))
	}
}
