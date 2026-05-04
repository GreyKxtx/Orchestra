package tools

import (
	"context"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/ckg"
)

func TestRunner_RuntimeQuery_JoinsCKG(t *testing.T) {
	r, _ := newEditRunner(t)
	ctx := context.Background()

	nodes := []ckg.Node{
		{FQN: "pkg.Handler", ShortName: "Handler", Kind: "func", LineStart: 5, LineEnd: 15},
	}
	if err := r.ckgStore.SaveFileNodes(ctx, "handler.go", "h1", "go", "ex", "pkg", nodes, nil); err != nil {
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
	if err := r.ckgStore.IngestTrace(ctx, td); err != nil {
		t.Fatalf("IngestTrace: %v", err)
	}

	resp, err := r.RuntimeQuery(ctx, RuntimeQueryRequest{TraceID: td.TraceID})
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

func TestRunner_RuntimeQuery_NotFound(t *testing.T) {
	r, _ := newEditRunner(t)
	ctx := context.Background()

	_, err := r.RuntimeQuery(ctx, RuntimeQueryRequest{TraceID: "deadbeefdeadbeefdeadbeefdeadbeef"})
	if err == nil {
		t.Fatal("expected error for missing trace, got nil")
	}
}
