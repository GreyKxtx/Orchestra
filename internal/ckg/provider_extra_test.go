package ckg

import (
	"context"
	"testing"
)

func TestProvider_Callees_Empty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	nodes := []Node{{FQN: "pkg.Foo", ShortName: "Foo", Kind: "func", LineStart: 1, LineEnd: 3}}
	if err := s.SaveFileNodes(ctx, "foo.go", "h1", "go", "pkg", "pkg", nodes, nil); err != nil {
		t.Fatal(err)
	}

	p := NewProvider(s, "/tmp")
	callees, err := p.Callees(ctx, "pkg.Foo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(callees) != 0 {
		t.Fatalf("expected no callees, got %v", callees)
	}
}

func TestProvider_Callees_ReturnsList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	save := func(file, fqn string, edges []Edge) {
		nodes := []Node{{FQN: fqn, ShortName: lastSegment(fqn), Kind: "func", LineStart: 1, LineEnd: 3}}
		if err := s.SaveFileNodes(ctx, file, "h", "go", "pkg", "pkg", nodes, edges); err != nil {
			t.Fatal(err)
		}
	}

	save("b.go", "pkg.B", nil)
	save("c.go", "pkg.C", nil)
	save("a.go", "pkg.A", []Edge{
		{SourceFQN: "pkg.A", TargetFQN: "pkg.B", Relation: "calls"},
		{SourceFQN: "pkg.A", TargetFQN: "pkg.C", Relation: "calls"},
	})

	p := NewProvider(s, "/tmp")
	callees, err := p.Callees(ctx, "pkg.A")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(callees) != 2 {
		t.Fatalf("expected 2 callees, got %d: %v", len(callees), callees)
	}
	for _, e := range callees {
		if e.SourceFQN != "pkg.A" {
			t.Errorf("unexpected source FQN: %q", e.SourceFQN)
		}
		if e.Relation != "calls" {
			t.Errorf("unexpected relation: %q", e.Relation)
		}
	}
}

func TestProvider_Callees_DoesNotReturnUnrelated(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	save := func(file, fqn string, edges []Edge) {
		nodes := []Node{{FQN: fqn, ShortName: lastSegment(fqn), Kind: "func", LineStart: 1, LineEnd: 2}}
		if err := s.SaveFileNodes(ctx, file, "h", "go", "pkg", "pkg", nodes, edges); err != nil {
			t.Fatal(err)
		}
	}
	save("x.go", "pkg.X", nil)
	save("y.go", "pkg.Y", []Edge{{SourceFQN: "pkg.Y", TargetFQN: "pkg.X", Relation: "calls"}})

	p := NewProvider(s, "/tmp")
	callees, err := p.Callees(ctx, "pkg.X") // X has no outgoing edges
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(callees) != 0 {
		t.Fatalf("expected no callees for X, got %v", callees)
	}
}
