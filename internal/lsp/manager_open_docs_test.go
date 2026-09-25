package lsp_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/lsp"
	"github.com/orchestra/orchestra/internal/lsp/lsptest"
)

type stagedPaths []string

func (s stagedPaths) EffectiveContent(string) (string, bool) { return "", false }
func (s stagedPaths) ListStagedPaths() []string              { return s }

func closesOf(t *testing.T, srv *lsptest.Server) <-chan string {
	t.Helper()
	closed := make(chan string, 8)
	srv.SetHandler("textDocument/didClose", func(params json.RawMessage) (json.RawMessage, error) {
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		}
		_ = json.Unmarshal(params, &p)
		closed <- p.TextDocument.URI
		return json.RawMessage(`null`), nil
	})
	return closed
}

func expectClose(t *testing.T, closed <-chan string, want string) {
	t.Helper()
	select {
	case got := <-closed:
		if got != want {
			t.Fatalf("closed %s, want %s", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("no didClose for %s", want)
	}
}

// A document opened for one edit stayed open for the life of the server;
// past the cap the least recently used one is closed — unless it is staged,
// because its content exists only in the overlay (DATA-8).
func TestManager_ClosesTheLeastRecentlyUsedDocumentPastTheCap(t *testing.T) {
	root := t.TempDir()
	conn, srv := lsptest.NewConn()
	c, err := lsp.StartFromConn("test", conn, lsp.PathToURI(root), nil)
	if err != nil {
		t.Fatal(err)
	}
	m := lsp.ForTest(root, c, []string{".go"}, 1500)
	t.Cleanup(m.Close)
	m.SetMaxOpenDocsForTest(2)
	closed := closesOf(t, srv)
	uri := func(rel string) string { return lsp.PathToURI(filepath.Join(root, rel)) }

	ctx := context.Background()
	for _, f := range []string{"a.go", "b.go", "c.go"} {
		if err := m.SyncStaged(ctx, f, "package p\n"); err != nil {
			t.Fatal(err)
		}
	}
	expectClose(t, closed, uri("a.go"))
	if c.IsOpen(uri("a.go")) || !c.IsOpen(uri("b.go")) || !c.IsOpen(uri("c.go")) {
		t.Fatalf("open: %v", c.OpenDocuments())
	}

	// b.go is touched again, so c.go is now the oldest; but c.go is staged
	// and stays: a.go's slot goes to d.go by closing b.go.
	if err := m.SyncStaged(ctx, "b.go", "package p\n// v2\n"); err != nil {
		t.Fatal(err)
	}
	m.SetContentProvider(stagedPaths{"c.go"})
	if err := m.SyncStaged(ctx, "d.go", "package p\n"); err != nil {
		t.Fatal(err)
	}
	expectClose(t, closed, uri("b.go"))
	if !c.IsOpen(uri("c.go")) || !c.IsOpen(uri("d.go")) || c.IsOpen(uri("b.go")) {
		t.Fatalf("open: %v", c.OpenDocuments())
	}

	// An explicit close leaves the bookkeeping consistent: nothing else is
	// evicted to make room.
	m.DidClose(ctx, "d.go")
	expectClose(t, closed, uri("d.go"))
	if err := m.SyncStaged(ctx, "e.go", "package p\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-closed:
		t.Fatalf("nothing needed closing, yet %s was", got)
	case <-time.After(200 * time.Millisecond):
	}
}
