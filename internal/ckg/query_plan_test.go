package ckg

import (
	"context"
	"strings"
	"testing"
)

// The one-hop lookups behind explore are index lookups on edges. They used
// to OR a joined column with an edge column, which no index serves, so every
// hop of every traversal scanned every edge (DATA-6). The query plan says
// which; this keeps it.
func TestNeighborQueries_UseTheEdgeIndexes(t *testing.T) {
	s := newTestStore(t)
	seedGraph(t, s, []Node{
		{FQN: "ex.A", ShortName: "A", Kind: "func", Package: "ex", LineStart: 1, LineEnd: 2},
		{FQN: "ex.B", ShortName: "B", Kind: "func", Package: "ex", LineStart: 3, LineEnd: 4},
	}, []Edge{{SourceFQN: "ex.A", TargetFQN: "ex.B", Relation: "calls"}})

	for name, q := range map[string]string{
		"upstream":   neighborsQuery(false, 2),
		"downstream": neighborsQuery(true, 2),
		"callers":    callersQuery,
	} {
		args := []any{"ex.B", "ex.B", "calls", "instantiates"}
		if name == "downstream" {
			args = []any{"ex.B", "calls", "instantiates"}
		}
		if name == "callers" {
			args = []any{"ex.B", "ex.B"}
		}
		rows, err := s.db.QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+q, args...)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, notused int
			var detail string
			if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, detail)
		}
		rows.Close()
		for _, step := range plan {
			if strings.HasPrefix(step, "SCAN e") || strings.HasPrefix(step, "SCAN edges") || strings.HasPrefix(step, "SCAN n") || strings.HasPrefix(step, "SCAN nodes") {
				t.Errorf("%s: a full scan on every hop: %q\nplan: %s", name, step, strings.Join(plan, " | "))
			}
		}
	}
}

// The rewrite keeps the answers: callers by resolved id and by FQN alone.
func TestNeighbors_UpstreamFindsCallersByIdAndByFQN(t *testing.T) {
	s := newTestStore(t)
	// A calls B (resolved: B is in the same save); C calls "ex.B" from a file
	// saved before B existed, so its edge is by FQN only until relinked.
	if err := s.SaveFileNodes(context.Background(), "c.go", "h", "go", "ex", "ex",
		[]Node{{FQN: "ex.C", ShortName: "C", Kind: "func", Package: "ex", LineStart: 1, LineEnd: 2}},
		[]Edge{{SourceFQN: "ex.C", TargetFQN: "ex.B", Relation: "calls"}}); err != nil {
		t.Fatal(err)
	}
	var dangling int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM edges WHERE target_id IS NULL`).Scan(&dangling)
	if dangling != 1 {
		t.Fatalf("C→ex.B should dangle before B exists: %d", dangling)
	}
	// B arrives in a save that must not relink C (SaveFileNodes step 5 does
	// relink by exact FQN — so drop that link again to test the FQN term).
	seedGraph(t, s, []Node{
		{FQN: "ex.A", ShortName: "A", Kind: "func", Package: "ex", LineStart: 1, LineEnd: 2},
		{FQN: "ex.B", ShortName: "B", Kind: "func", Package: "ex", LineStart: 3, LineEnd: 4},
	}, []Edge{{SourceFQN: "ex.A", TargetFQN: "ex.B", Relation: "calls"}})
	if _, err := s.db.Exec(`UPDATE edges SET target_id = NULL WHERE source_id = (SELECT id FROM nodes WHERE fqn = 'ex.C')`); err != nil {
		t.Fatal(err)
	}
	ns, err := s.neighbors(context.Background(), "ex.B", false, []string{"calls"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, n := range ns {
		got[n.node.FQN] = true
	}
	if !got["ex.A"] || !got["ex.C"] {
		t.Fatalf("upstream of B must list A (by id) and C (by FQN): %v", got)
	}
}
