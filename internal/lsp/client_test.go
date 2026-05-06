package lsp_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/lsp"
	"github.com/orchestra/orchestra/internal/lsp/lsptest"
)

func TestClient_Initialize_PosEncoding(t *testing.T) {
	conn, _ := lsptest.NewConn()
	c, err := lsp.StartFromConn("test", conn, "file:///workspace", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.PosEncoding() != "utf-8" {
		t.Fatalf("expected utf-8, got %q", c.PosEncoding())
	}
}

func TestClient_RequestResponse(t *testing.T) {
	conn, srv := lsptest.NewConn()
	srv.SetHandler("workspace/symbol", func(_ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`[{"name":"TestFunc"}]`), nil
	})
	c, err := lsp.StartFromConn("test", conn, "file:///workspace", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	raw, err := c.Request(context.Background(), "workspace/symbol", map[string]string{"query": "TestFunc"})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `[{"name":"TestFunc"}]` {
		t.Fatalf("unexpected response: %s", raw)
	}
}

func TestClient_Notification(t *testing.T) {
	conn, srv := lsptest.NewConn()
	c, err := lsp.StartFromConn("test", conn, "file:///workspace", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	go func() {
		time.Sleep(10 * time.Millisecond)
		_ = srv.PushDiagnostics("file:///test.go", []lsp.Diagnostic{
			{Message: "undefined: foo"},
		})
	}()

	select {
	case msg := <-c.Notifications():
		if msg.Method != "textDocument/publishDiagnostics" {
			t.Fatalf("unexpected method: %q", msg.Method)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for notification")
	}
}

func TestClient_IsOpen(t *testing.T) {
	conn, _ := lsptest.NewConn()
	c, err := lsp.StartFromConn("test", conn, "file:///workspace", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	const uri = "file:///workspace/main.go"
	if c.IsOpen(uri) {
		t.Fatal("should not be open before DidOpen")
	}
	if err := c.DidOpen(context.Background(), uri, "go", "package main"); err != nil {
		t.Fatal(err)
	}
	if !c.IsOpen(uri) {
		t.Fatal("should be open after DidOpen")
	}
	if err := c.DidClose(context.Background(), uri); err != nil {
		t.Fatal(err)
	}
	if c.IsOpen(uri) {
		t.Fatal("should not be open after DidClose")
	}
}

func TestClient_DeadAfterClose(t *testing.T) {
	conn, _ := lsptest.NewConn()
	c, err := lsp.StartFromConn("test", conn, "file:///workspace", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	// Give readLoop time to set dead.
	time.Sleep(50 * time.Millisecond)
	if !c.IsDead() {
		t.Fatal("expected client to be dead after Close")
	}
}

func TestClient_RequestAfterDead(t *testing.T) {
	conn, _ := lsptest.NewConn()
	c, err := lsp.StartFromConn("test", conn, "file:///workspace", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	time.Sleep(50 * time.Millisecond)

	_, err = c.Request(context.Background(), "workspace/symbol", nil)
	if err == nil {
		t.Fatal("expected error after close")
	}
}
