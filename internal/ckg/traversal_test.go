package ckg

import (
	"context"
	"strings"
	"testing"
)

func seedGraph(t *testing.T, s *Store, nodes []Node, edges []Edge) {
	t.Helper()
	ctx := context.Background()
	if err := s.SaveFileNodes(ctx, "graph.go", "h", "go", "ex", "ex", nodes, edges); err != nil {
		t.Fatalf("SaveFileNodes: %v", err)
	}
}

func TestTraverseBFS_CycleDoesNotLoop(t *testing.T) {
	s := newTestStore(t)
	seedGraph(t, s, []Node{
		{FQN: "ex.A", ShortName: "A", Kind: "func", Package: "ex", LineStart: 1, LineEnd: 2},
		{FQN: "ex.B", ShortName: "B", Kind: "func", Package: "ex", LineStart: 3, LineEnd: 4},
		{FQN: "ex.C", ShortName: "C", Kind: "func", Package: "ex", LineStart: 5, LineEnd: 6},
	}, []Edge{
		{SourceFQN: "ex.A", TargetFQN: "ex.B", Relation: "calls"},
		{SourceFQN: "ex.B", TargetFQN: "ex.C", Relation: "calls"},
		{SourceFQN: "ex.C", TargetFQN: "ex.A", Relation: "calls"},
	})

	sg, err := s.TraverseBFS(context.Background(), "ex.A", DirectionDownstream, TraversalOptions{MaxDepth: 8, MaxNodes: 50})
	if err != nil {
		t.Fatalf("TraverseBFS: %v", err)
	}
	if len(sg.Nodes) != 3 {
		t.Fatalf("cycle graph: got %d nodes, want 3 (A,B,C)", len(sg.Nodes))
	}
	if sg.Depth["ex.A"] != 0 || sg.Depth["ex.B"] != 1 || sg.Depth["ex.C"] != 2 {
		t.Fatalf("depths = %+v", sg.Depth)
	}
	// Back-edge C→A is recorded without re-expanding A.
	foundBack := false
	for _, e := range sg.Edges {
		if e.SourceFQN == "ex.C" && e.TargetFQN == "ex.A" {
			foundBack = true
		}
	}
	if !foundBack {
		t.Fatal("expected cycle back-edge C→A in subgraph")
	}

	path, err := s.FindPath(context.Background(), "ex.A", "ex.C", 4)
	if err != nil {
		t.Fatalf("FindPath: %v", err)
	}
	if path == nil || len(path.Nodes) != 3 {
		t.Fatalf("FindPath A→C: %+v", path)
	}
}

func TestTraverseBFS_DiamondMinDepth(t *testing.T) {
	s := newTestStore(t)
	seedGraph(t, s, []Node{
		{FQN: "ex.A", ShortName: "A", Kind: "func", Package: "ex", LineStart: 1, LineEnd: 2},
		{FQN: "ex.B", ShortName: "B", Kind: "func", Package: "ex", LineStart: 3, LineEnd: 4},
		{FQN: "ex.C", ShortName: "C", Kind: "func", Package: "ex", LineStart: 5, LineEnd: 6},
		{FQN: "ex.D", ShortName: "D", Kind: "func", Package: "ex", LineStart: 7, LineEnd: 8},
	}, []Edge{
		{SourceFQN: "ex.A", TargetFQN: "ex.B", Relation: "calls"},
		{SourceFQN: "ex.A", TargetFQN: "ex.C", Relation: "calls"},
		{SourceFQN: "ex.B", TargetFQN: "ex.D", Relation: "calls"},
		{SourceFQN: "ex.C", TargetFQN: "ex.D", Relation: "calls"},
	})

	sg, err := s.TraverseBFS(context.Background(), "ex.A", DirectionDownstream, TraversalOptions{MaxDepth: 4, MaxNodes: 50})
	if err != nil {
		t.Fatalf("TraverseBFS: %v", err)
	}
	if len(sg.Nodes) != 4 {
		t.Fatalf("diamond: got %d nodes, want 4", len(sg.Nodes))
	}
	if d := sg.Depth["ex.D"]; d != 2 {
		t.Fatalf("D depth = %d, want 2 (shortest path A→B→D or A→C→D)", d)
	}
	// Both incoming edges to D are kept.
	intoD := 0
	for _, e := range sg.Edges {
		if e.TargetFQN == "ex.D" {
			intoD++
		}
	}
	if intoD != 2 {
		t.Fatalf("edges into D = %d, want 2", intoD)
	}
}

func TestTraverseBFS_MaxDepthAndMaxNodes(t *testing.T) {
	s := newTestStore(t)
	seedGraph(t, s, []Node{
		{FQN: "ex.A", ShortName: "A", Kind: "func", Package: "ex", LineStart: 1, LineEnd: 2},
		{FQN: "ex.B", ShortName: "B", Kind: "func", Package: "ex", LineStart: 3, LineEnd: 4},
		{FQN: "ex.C", ShortName: "C", Kind: "func", Package: "ex", LineStart: 5, LineEnd: 6},
		{FQN: "ex.D", ShortName: "D", Kind: "func", Package: "ex", LineStart: 7, LineEnd: 8},
	}, []Edge{
		{SourceFQN: "ex.A", TargetFQN: "ex.B", Relation: "calls"},
		{SourceFQN: "ex.B", TargetFQN: "ex.C", Relation: "calls"},
		{SourceFQN: "ex.C", TargetFQN: "ex.D", Relation: "calls"},
	})

	sg, err := s.TraverseBFS(context.Background(), "ex.A", DirectionDownstream, TraversalOptions{MaxDepth: 1, MaxNodes: 50})
	if err != nil {
		t.Fatalf("TraverseBFS depth: %v", err)
	}
	if _, ok := sg.Nodes["ex.C"]; ok {
		t.Fatal("MaxDepth=1 must not include C (depth 2)")
	}
	if _, ok := sg.Nodes["ex.B"]; !ok {
		t.Fatal("MaxDepth=1 must include B")
	}

	starNodes := []Node{{FQN: "ex.Root", ShortName: "Root", Kind: "func", Package: "ex", LineStart: 1, LineEnd: 2}}
	var starEdges []Edge
	for i, name := range []string{"ex.N1", "ex.N2", "ex.N3", "ex.N4", "ex.N5"} {
		starNodes = append(starNodes, Node{FQN: name, ShortName: name, Kind: "func", Package: "ex", LineStart: 10 + i, LineEnd: 11 + i})
		starEdges = append(starEdges, Edge{SourceFQN: "ex.Root", TargetFQN: name, Relation: "calls"})
	}
	s2 := newTestStore(t)
	seedGraph(t, s2, starNodes, starEdges)
	capped, err := s2.TraverseBFS(context.Background(), "ex.Root", DirectionDownstream, TraversalOptions{MaxDepth: 3, MaxNodes: 3})
	if err != nil {
		t.Fatalf("TraverseBFS nodes: %v", err)
	}
	if len(capped.Nodes) > 3 {
		t.Fatalf("MaxNodes=3: got %d nodes", len(capped.Nodes))
	}
}

func TestFormatSubgraphContext_TreeXML(t *testing.T) {
	s := newTestStore(t)
	seedGraph(t, s, []Node{
		{FQN: "ex.ValidateToken", ShortName: "ValidateToken", Kind: "func", Package: "auth", LineStart: 10, LineEnd: 20},
		{FQN: "ex.HandleAPI", ShortName: "HandleAPI", Kind: "func", Package: "router", LineStart: 45, LineEnd: 50},
		{FQN: "ex.ParseClaims", ShortName: "ParseClaims", Kind: "func", Package: "jwt", LineStart: 12, LineEnd: 18},
		{FQN: "ex.TokenClaims", ShortName: "TokenClaims", Kind: "struct", Package: "auth", LineStart: 3, LineEnd: 8},
	}, []Edge{
		{SourceFQN: "ex.HandleAPI", TargetFQN: "ex.ValidateToken", Relation: "calls"},
		{SourceFQN: "ex.ValidateToken", TargetFQN: "ex.ParseClaims", Relation: "calls"},
		{SourceFQN: "ex.ValidateToken", TargetFQN: "ex.TokenClaims", Relation: "instantiates"},
	})

	sg, err := s.TraverseBFS(context.Background(), "ex.ValidateToken", DirectionBoth, TraversalOptions{MaxDepth: 2, MaxNodes: 50, IncludeTypes: true})
	if err != nil {
		t.Fatalf("TraverseBFS: %v", err)
	}
	out := FormatSubgraphContext(sg, 800)
	if !strings.Contains(out, "<ckg_subgraph") || !strings.Contains(out, "</ckg_subgraph>") {
		t.Fatalf("missing XML wrapper:\n%s", out)
	}
	if !strings.Contains(out, "callers:") || !strings.Contains(out, "callees:") {
		t.Fatalf("missing callers/callees branches:\n%s", out)
	}
	if !strings.Contains(out, "[instantiates]") {
		t.Fatalf("missing instantiates marker:\n%s", out)
	}
	if !strings.Contains(out, "HandleAPI") || !strings.Contains(out, "ParseClaims") {
		t.Fatalf("missing neighbor labels:\n%s", out)
	}

	tiny := FormatSubgraphContext(sg, 1)
	if !strings.Contains(tiny, "<ckg_subgraph") {
		t.Fatalf("tiny budget should still wrap: %q", tiny)
	}
}
