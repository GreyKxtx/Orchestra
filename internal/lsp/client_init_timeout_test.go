package lsp_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/lsp"
	"github.com/orchestra/orchestra/internal/lsp/lsptest"
)

func TestStartFromConn_InitializeTimeout(t *testing.T) {
	conn, srv := lsptest.NewConn()
	block := make(chan struct{})
	srv.SetHandler("initialize", func(_ json.RawMessage) (json.RawMessage, error) {
		<-block
		return json.RawMessage(`{"capabilities":{}}`), nil
	})
	t.Cleanup(func() { close(block) })

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := lsp.StartFromConnContext(ctx, "test", conn, "file:///workspace", nil, 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected initialize timeout error")
	}
	if !strings.Contains(err.Error(), "initialize") {
		t.Fatalf("unexpected error: %v", err)
	}
}
