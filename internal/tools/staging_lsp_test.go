package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/cache"
	"github.com/orchestra/orchestra/internal/lsp"
	"github.com/orchestra/orchestra/internal/lsp/lsptest"
)

func newDryRunRunnerWithMockLSP(t *testing.T) (*Runner, *lsptest.Server) {
	t.Helper()
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{DryRun: true})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	conn, srv := lsptest.NewConn()
	c, err := lsp.StartFromConn("test", conn, lsp.PathToURI(root), nil)
	if err != nil {
		t.Fatalf("StartFromConn: %v", err)
	}
	r.lspManager = lsp.ForTest(root, c, []string{".go"}, 1500)
	r.lspManager.SetContentProvider(r)

	t.Cleanup(func() {
		r.lspManager.Close()
		r.Close()
	})
	return r, srv
}

func TestStaging_LSP_SyncStagedOnDryRunWrite(t *testing.T) {
	r, srv := newDryRunRunnerWithMockLSP(t)

	diskContent := "package main\n\nfunc Old() {}\n"
	stagedContent := "package main\n\nfunc Staged() {}\n"
	path := "main.go"
	if err := os.WriteFile(filepath.Join(r.workspaceRoot, path), []byte(diskContent), 0644); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	opened := make(chan string, 1)
	srv.SetHandler("textDocument/didOpen", func(params json.RawMessage) (json.RawMessage, error) {
		var p struct {
			TextDocument struct {
				Text string `json:"text"`
			} `json:"textDocument"`
		}
		_ = json.Unmarshal(params, &p)
		mu.Lock()
		defer mu.Unlock()
		select {
		case opened <- p.TextDocument.Text:
		default:
		}
		return json.RawMessage(`null`), nil
	})

	_, err := r.FSWrite(context.Background(), FSWriteRequest{
		Path:     path,
		Content:  stagedContent,
		FileHash: cache.ComputeSHA256([]byte(diskContent)),
	})
	if err != nil {
		t.Fatalf("FSWrite dry-run: %v", err)
	}

	var got string
	select {
	case got = <-opened:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for textDocument/didOpen")
	}
	if got != stagedContent {
		t.Fatalf("didOpen text: got %q, want staged %q", got, stagedContent)
	}

	onDisk, err := os.ReadFile(filepath.Join(r.workspaceRoot, path))
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != diskContent {
		t.Fatalf("disk modified: got %q", string(onDisk))
	}
}

func TestStaging_LSP_DidChangeOnSecondStage(t *testing.T) {
	r, srv := newDryRunRunnerWithMockLSP(t)

	path := "main.go"
	first := "package main\n\nfunc First() {}\n"
	second := "package main\n\nfunc Second() {}\n"
	disk := "package main\n\nfunc Old() {}\n"
	if err := os.WriteFile(filepath.Join(r.workspaceRoot, path), []byte(disk), 0644); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var changes []string
	notify := make(chan struct{}, 4)
	record := func(text string) {
		mu.Lock()
		changes = append(changes, text)
		mu.Unlock()
		select {
		case notify <- struct{}{}:
		default:
		}
	}
	srv.SetHandler("textDocument/didOpen", func(params json.RawMessage) (json.RawMessage, error) {
		var p struct {
			TextDocument struct {
				Text string `json:"text"`
			} `json:"textDocument"`
		}
		_ = json.Unmarshal(params, &p)
		record(p.TextDocument.Text)
		return json.RawMessage(`null`), nil
	})
	srv.SetHandler("textDocument/didChange", func(params json.RawMessage) (json.RawMessage, error) {
		var p struct {
			ContentChanges []struct {
				Text string `json:"text"`
			} `json:"contentChanges"`
		}
		_ = json.Unmarshal(params, &p)
		if len(p.ContentChanges) > 0 {
			record(p.ContentChanges[0].Text)
		}
		return json.RawMessage(`null`), nil
	})

	hash := cache.ComputeSHA256([]byte(disk))
	if _, err := r.FSEdit(context.Background(), FSEditRequest{
		Path:     path,
		FileHash: hash,
		Search:   "Old",
		Replace:  "First",
	}); err != nil {
		t.Fatalf("first FSEdit: %v", err)
	}
	select {
	case <-notify:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for first LSP sync")
	}
	if _, err := r.FSEdit(context.Background(), FSEditRequest{
		Path:     path,
		FileHash: r.currentHash(path),
		Search:   "First()",
		Replace:  "Second()",
	}); err != nil {
		t.Fatalf("second FSEdit: %v", err)
	}
	select {
	case <-notify:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for second LSP sync")
	}

	mu.Lock()
	got := append([]string(nil), changes...)
	mu.Unlock()
	if len(got) < 2 {
		t.Fatalf("expected didOpen + didChange, got %d notifications: %v", len(got), got)
	}
	if got[0] != first {
		t.Errorf("didOpen: got %q", got[0])
	}
	if got[len(got)-1] != second {
		t.Errorf("last change: got %q, want %q", got[len(got)-1], second)
	}
}
