package ckg

import (
	"context"
	"testing"
)

func TestBuildGraphData_Empty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	data, err := buildGraphData(ctx, s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data == nil {
		t.Fatal("got nil GraphData")
	}
	if len(data.Nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(data.Nodes))
	}
	if len(data.Links) != 0 {
		t.Errorf("expected 0 links, got %d", len(data.Links))
	}
}

func TestBuildGraphData_WithNodes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	nodes := []Node{
		{FQN: "pkg.Foo", ShortName: "Foo", Kind: "func", LineStart: 1, LineEnd: 5},
		{FQN: "pkg.Bar", ShortName: "Bar", Kind: "struct", LineStart: 7, LineEnd: 10},
	}
	if err := s.SaveFileNodes(ctx, "main.go", "sha256:x", "go", "pkg", "pkg", nodes, nil); err != nil {
		t.Fatal(err)
	}

	data, err := buildGraphData(ctx, s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have: 1 file node + 2 symbol nodes = 3 total
	if len(data.Nodes) != 3 {
		t.Fatalf("expected 3 nodes (1 file + 2 symbols), got %d: %v", len(data.Nodes), data.Nodes)
	}

	groups := map[string]int{}
	for _, n := range data.Nodes {
		groups[n.Group]++
	}
	if groups["file"] != 1 {
		t.Errorf("expected 1 file node, got %d", groups["file"])
	}
	if groups["func"] != 1 {
		t.Errorf("expected 1 func node, got %d", groups["func"])
	}
	if groups["struct"] != 1 {
		t.Errorf("expected 1 struct node, got %d", groups["struct"])
	}
}

func TestBuildGraphData_SkipsVendorAndTestdata(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	nodes := []Node{{FQN: "v.Foo", ShortName: "Foo", Kind: "func", LineStart: 1, LineEnd: 2}}
	// Save into vendor and testdata paths.
	if err := s.SaveFileNodes(ctx, "vendor/lib/lib.go", "h1", "go", "v", "v", nodes, nil); err != nil {
		t.Fatal(err)
	}
	tdNodes := []Node{{FQN: "td.Bar", ShortName: "Bar", Kind: "func", LineStart: 1, LineEnd: 2}}
	if err := s.SaveFileNodes(ctx, "testdata/sample.go", "h2", "go", "td", "td", tdNodes, nil); err != nil {
		t.Fatal(err)
	}

	data, err := buildGraphData(ctx, s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// vendor and testdata paths should be excluded.
	for _, n := range data.Nodes {
		if n.ID == "vendor/lib/lib.go" || n.ID == "testdata/sample.go" {
			t.Errorf("vendor/testdata node leaked into graph: %v", n)
		}
	}
}

func TestBuildGraphData_WithEdges(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	callerNodes := []Node{{FQN: "pkg.A", ShortName: "A", Kind: "func", LineStart: 1, LineEnd: 3}}
	calleeNodes := []Node{{FQN: "pkg.B", ShortName: "B", Kind: "func", LineStart: 5, LineEnd: 7}}

	if err := s.SaveFileNodes(ctx, "b.go", "h1", "go", "pkg", "pkg", calleeNodes, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveFileNodes(ctx, "a.go", "h2", "go", "pkg", "pkg", callerNodes,
		[]Edge{{SourceFQN: "pkg.A", TargetFQN: "pkg.B", Relation: "calls"}}); err != nil {
		t.Fatal(err)
	}

	data, err := buildGraphData(ctx, s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have at least one link between pkg.A and pkg.B.
	found := false
	for _, l := range data.Links {
		if l.Source == "pkg.A" && l.Target == "pkg.B" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected link pkg.A → pkg.B; links: %v", data.Links)
	}
}

func TestBuildGraphData_TestFunctionGroup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	nodes := []Node{
		{FQN: "pkg.TestFoo", ShortName: "TestFoo", Kind: "func", LineStart: 1, LineEnd: 5},
	}
	if err := s.SaveFileNodes(ctx, "foo_test.go", "h", "go", "pkg", "pkg", nodes, nil); err != nil {
		t.Fatal(err)
	}

	data, err := buildGraphData(ctx, s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, n := range data.Nodes {
		if n.ID == "pkg.TestFoo" && n.Group != "test" {
			t.Errorf("TestFoo should have group 'test', got %q", n.Group)
		}
	}
}
