package ckg

import (
	"context"
	"strings"
	"testing"
)

func TestProviderCallersChain(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// A → B → C
	save := func(file, fqn string, edges []Edge) {
		nodes := []Node{{FQN: fqn, ShortName: lastSegment(fqn), Kind: "func", LineStart: 1, LineEnd: 3}}
		if err := s.SaveFileNodes(ctx, file, "h", "go", "ex", "ex", nodes, edges); err != nil {
			t.Fatal(err)
		}
	}
	save("c.go", "ex.C", nil)
	save("b.go", "ex.B", []Edge{{SourceFQN: "ex.B", TargetFQN: "ex.C", Relation: "calls"}})
	save("a.go", "ex.A", []Edge{{SourceFQN: "ex.A", TargetFQN: "ex.B", Relation: "calls"}})

	p := NewProvider(s, "/tmp")
	callers, err := p.Callers(ctx, "ex.C")
	if err != nil {
		t.Fatal(err)
	}
	if len(callers) != 1 || callers[0].FQN != "ex.B" {
		t.Fatalf("direct callers of C: got %+v, want [ex.B]", callers)
	}

	callers2, err := p.Callers(ctx, "ex.B")
	if err != nil {
		t.Fatal(err)
	}
	if len(callers2) != 1 || callers2[0].FQN != "ex.A" {
		t.Fatalf("direct callers of B: got %+v, want [ex.A]", callers2)
	}
}

func TestProviderImporters(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	save := func(file, pkgFQN string, edges []Edge) {
		nodes := []Node{{FQN: pkgFQN, ShortName: lastSegment(pkgFQN), Kind: "package", LineStart: 1, LineEnd: 1}}
		if err := s.SaveFileNodes(ctx, file, "h", "go", "ex", "pkg", nodes, edges); err != nil {
			t.Fatal(err)
		}
	}
	save("auth.go", "ex/auth", nil)
	save("api.go", "ex/api", []Edge{{SourceFQN: "ex/api", TargetFQN: "ex/auth", Relation: "imports"}})
	save("svc.go", "ex/svc", []Edge{{SourceFQN: "ex/svc", TargetFQN: "ex/auth", Relation: "imports"}})

	p := NewProvider(s, "/tmp")
	imps, err := p.Importers(ctx, "ex/auth")
	if err != nil {
		t.Fatal(err)
	}
	if len(imps) != 2 {
		t.Fatalf("importers of ex/auth: got %v, want 2", imps)
	}
}

func TestExploreSymbolAmbiguousShortName(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Two distinct Run functions in different packages — same short_name.
	save := func(file, fqn string) {
		nodes := []Node{{FQN: fqn, ShortName: "Run", Kind: "func", LineStart: 1, LineEnd: 3}}
		if err := s.SaveFileNodes(ctx, file, "h", "go", "ex", "p", nodes, nil); err != nil {
			t.Fatal(err)
		}
	}
	save("a.go", "ex/a.Run")
	save("b.go", "ex/b.Run")

	p := NewProvider(s, "/tmp")
	out, err := p.ExploreSymbol(ctx, "Run")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "неоднозначен") {
		t.Fatalf("expected ambiguity message, got: %s", out)
	}
	if !strings.Contains(out, "ex/a.Run") || !strings.Contains(out, "ex/b.Run") {
		t.Fatalf("expected both FQNs listed, got: %s", out)
	}
}

func lastSegment(fqn string) string {
	if i := strings.LastIndex(fqn, "."); i >= 0 {
		return fqn[i+1:]
	}
	if i := strings.LastIndex(fqn, "/"); i >= 0 {
		return fqn[i+1:]
	}
	return fqn
}
