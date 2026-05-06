package lsp_test

import (
	"context"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/lsp"
)

func TestDiagnosticsCache_NilBeforeUpdate(t *testing.T) {
	dc := lsp.NewDiagnosticsCache()
	if dc.Get("file:///foo.go") != nil {
		t.Fatal("expected nil before any update")
	}
}

func TestDiagnosticsCache_UpdateAndGet(t *testing.T) {
	dc := lsp.NewDiagnosticsCache()
	diags := []lsp.Diagnostic{{Message: "test error"}}
	dc.Update("file:///foo.go", diags)
	got := dc.Get("file:///foo.go")
	if len(got) != 1 || got[0].Message != "test error" {
		t.Fatalf("unexpected: %v", got)
	}
}

func TestDiagnosticsCache_NilBecomesEmpty(t *testing.T) {
	dc := lsp.NewDiagnosticsCache()
	dc.Update("file:///foo.go", nil)
	got := dc.Get("file:///foo.go")
	if got == nil {
		t.Fatal("expected empty slice, not nil")
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d", len(got))
	}
}

func TestDiagnosticsCache_WaitForUpdate(t *testing.T) {
	dc := lsp.NewDiagnosticsCache()
	diags := []lsp.Diagnostic{{Message: "async error"}}

	done := make(chan []lsp.Diagnostic, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		done <- dc.WaitForUpdate(ctx, "file:///foo.go")
	}()

	time.Sleep(10 * time.Millisecond)
	dc.Update("file:///foo.go", diags)

	got := <-done
	if len(got) != 1 || got[0].Message != "async error" {
		t.Fatalf("unexpected: %v", got)
	}
}

func TestDiagnosticsCache_WaitCancelled(t *testing.T) {
	dc := lsp.NewDiagnosticsCache()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	got := dc.WaitForUpdate(ctx, "file:///foo.go")
	if got != nil {
		t.Fatalf("expected nil on timeout, got %v", got)
	}
}

func TestDiagnosticsCache_HandleNotification(t *testing.T) {
	dc := lsp.NewDiagnosticsCache()
	raw := []byte(`{"uri":"file:///bar.go","diagnostics":[{"message":"oops","severity":1}]}`)
	dc.HandleNotification(raw)
	got := dc.Get("file:///bar.go")
	if len(got) != 1 || got[0].Message != "oops" || got[0].Severity != lsp.SeverityError {
		t.Fatalf("unexpected: %v", got)
	}
}

func TestDiagnosticsCache_MultipleWaiters(t *testing.T) {
	dc := lsp.NewDiagnosticsCache()
	const n = 3
	results := make(chan []lsp.Diagnostic, n)
	for i := 0; i < n; i++ {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			results <- dc.WaitForUpdate(ctx, "file:///multi.go")
		}()
	}
	time.Sleep(10 * time.Millisecond)
	dc.Update("file:///multi.go", []lsp.Diagnostic{{Message: "x"}})
	// Only one waiter receives the update.
	select {
	case got := <-results:
		if len(got) != 1 {
			t.Fatalf("unexpected: %v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}
