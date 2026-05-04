package ckg

import (
	"context"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	tmp := t.TempDir()
	s, err := NewStore(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMigrateToV3(t *testing.T) {
	s := newTestStore(t)
	var v int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != 3 {
		t.Fatalf("user_version = %d, want 3", v)
	}

	// Schema sanity: nodes.fqn column exists.
	var dummy string
	err := s.db.QueryRow(`SELECT fqn FROM nodes LIMIT 1`).Scan(&dummy)
	if err != nil && err.Error() != "sql: no rows in result set" {
		t.Fatalf("nodes.fqn missing or unreadable: %v", err)
	}

	// Schema sanity: traces and spans tables exist.
	err = s.db.QueryRow(`SELECT id FROM traces LIMIT 1`).Scan(&dummy)
	if err != nil && err.Error() != "sql: no rows in result set" {
		t.Fatalf("traces table missing or unreadable: %v", err)
	}
	err = s.db.QueryRow(`SELECT span_id FROM spans LIMIT 1`).Scan(&dummy)
	if err != nil && err.Error() != "sql: no rows in result set" {
		t.Fatalf("spans table missing or unreadable: %v", err)
	}
}

func TestSaveFileNodesAndLazyResolve(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// 1. Save A first; A calls B (B not yet indexed).
	nodesA := []Node{
		{FQN: "ex/foo.A", ShortName: "A", Kind: "func", LineStart: 1, LineEnd: 3},
	}
	edgesA := []Edge{
		{SourceFQN: "ex/foo.A", TargetFQN: "ex/foo.B", Relation: "calls"},
	}
	if err := s.SaveFileNodes(ctx, "foo.go", "h1", "go", "ex", "foo", nodesA, edgesA); err != nil {
		t.Fatalf("save A: %v", err)
	}

	// After saving A only, the edge to B has target_id NULL.
	var targetID *int64
	err := s.db.QueryRowContext(ctx,
		`SELECT target_id FROM edges WHERE target_fqn = ?`, "ex/foo.B").Scan(&targetID)
	if err != nil {
		t.Fatal(err)
	}
	if targetID != nil {
		t.Fatalf("target_id should be NULL before B is indexed, got %d", *targetID)
	}

	// 2. Save B in a different file. Lazy resolution must update the existing edge.
	nodesB := []Node{
		{FQN: "ex/foo.B", ShortName: "B", Kind: "func", LineStart: 1, LineEnd: 3},
	}
	if err := s.SaveFileNodes(ctx, "bar.go", "h2", "go", "ex", "foo", nodesB, nil); err != nil {
		t.Fatalf("save B: %v", err)
	}

	err = s.db.QueryRowContext(ctx,
		`SELECT target_id FROM edges WHERE target_fqn = ?`, "ex/foo.B").Scan(&targetID)
	if err != nil {
		t.Fatal(err)
	}
	if targetID == nil {
		t.Fatal("target_id still NULL after lazy resolve")
	}
}

func TestSaveFileNodesResolvesUniqueShortNameCallTarget(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// First index target symbol B.
	nodesB := []Node{
		{FQN: "ex/foo.B", ShortName: "B", Kind: "func", LineStart: 1, LineEnd: 3},
	}
	if err := s.SaveFileNodes(ctx, "b.go", "h1", "go", "ex", "foo", nodesB, nil); err != nil {
		t.Fatalf("save B: %v", err)
	}

	// Parser-style edge: short name target ("B"), not FQN.
	nodesA := []Node{
		{FQN: "ex/foo.A", ShortName: "A", Kind: "func", LineStart: 1, LineEnd: 3},
	}
	edgesA := []Edge{
		{SourceFQN: "ex/foo.A", TargetFQN: "B", Relation: "calls"},
	}
	if err := s.SaveFileNodes(ctx, "a.go", "h2", "go", "ex", "foo", nodesA, edgesA); err != nil {
		t.Fatalf("save A: %v", err)
	}

	var targetID *int64
	var targetFQN string
	err := s.db.QueryRowContext(ctx, `SELECT target_id, target_fqn FROM edges WHERE relation = 'calls'`).Scan(&targetID, &targetFQN)
	if err != nil {
		t.Fatal(err)
	}
	if targetID == nil {
		t.Fatal("expected target_id resolved for unique short_name, got NULL")
	}
	if targetFQN != "ex/foo.B" {
		t.Fatalf("target_fqn = %q, want ex/foo.B", targetFQN)
	}
}
