package session

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/ckg"
	"github.com/orchestra/orchestra/internal/memory"
)

func TestRuntimeQuery_JoinsCKG(t *testing.T) {
	root := t.TempDir()
	store, err := ckg.NewStore(filepath.Join(root, "ckg.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	nodes := []ckg.Node{
		{FQN: "pkg.Handler", ShortName: "Handler", Kind: "func", LineStart: 5, LineEnd: 15},
	}
	if err := store.SaveFileNodes(ctx, "handler.go", "h1", "go", "ex", "pkg", nodes, nil); err != nil {
		t.Fatalf("SaveFileNodes: %v", err)
	}

	td := ckg.TraceData{
		TraceID:   "aabb00112233445566778899aabb0011",
		Service:   "mysvc",
		StartedAt: time.Now(),
		Spans: []ckg.SpanData{
			{SpanID: "s001", Name: "handle", CodeFile: "handler.go", CodeLineno: 10},
		},
	}
	if err := store.IngestTrace(ctx, td); err != nil {
		t.Fatalf("IngestTrace: %v", err)
	}

	c := NewClient(root, func() string { return "" }, func() memory.Config { return memory.DefaultConfig() }, func() *ckg.Store { return store })

	resp, err := c.RuntimeQuery(ctx, RuntimeQueryRequest{TraceID: td.TraceID})
	if err != nil {
		t.Fatalf("RuntimeQuery: %v", err)
	}
	if resp.Service != "mysvc" {
		t.Errorf("Service = %q, want mysvc", resp.Service)
	}
	if len(resp.Spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(resp.Spans))
	}
	sp := resp.Spans[0]
	if sp.NodeFQN != "pkg.Handler" {
		t.Errorf("NodeFQN = %q, want pkg.Handler", sp.NodeFQN)
	}
	if sp.NodeKind != "func" {
		t.Errorf("NodeKind = %q, want func", sp.NodeKind)
	}
	if sp.ResolveStatus != ckg.ResolveStatusResolved {
		t.Errorf("ResolveStatus = %q, want %q", sp.ResolveStatus, ckg.ResolveStatusResolved)
	}
}

func TestRuntimeQuery_NotFound(t *testing.T) {
	root := t.TempDir()
	store, err := ckg.NewStore(filepath.Join(root, "ckg.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	c := NewClient(root, func() string { return "" }, func() memory.Config { return memory.DefaultConfig() }, func() *ckg.Store { return store })

	_, err = c.RuntimeQuery(context.Background(), RuntimeQueryRequest{TraceID: "deadbeefdeadbeefdeadbeefdeadbeef"})
	if err == nil {
		t.Fatal("expected error for missing trace, got nil")
	}
}
