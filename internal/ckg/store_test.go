package ckg

import (
	"context"
	"database/sql"
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

func TestMigrateToV5(t *testing.T) {
	s := newTestStore(t)
	var v int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != 5 {
		t.Fatalf("user_version = %d, want 5", v)
	}
	// node_embeddings table exists.
	var name string
	if err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='node_embeddings'`).Scan(&name); err != nil {
		t.Fatalf("node_embeddings table missing: %v", err)
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

	for _, idx := range []string{"idx_nodes_package", "idx_edges_source_rel", "idx_edges_target_rel"} {
		var iname string
		err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, idx).Scan(&iname)
		if err != nil {
			t.Fatalf("index %s missing: %v", idx, err)
		}
	}
	var dummyInt int
	if err := s.db.QueryRow(`SELECT is_external FROM edges LIMIT 1`).Scan(&dummyInt); err != nil && err.Error() != "sql: no rows in result set" {
		t.Fatalf("edges.is_external missing: %v", err)
	}
	if err := s.db.QueryRow(`SELECT package FROM nodes LIMIT 1`).Scan(&dummy); err != nil && err.Error() != "sql: no rows in result set" {
		t.Fatalf("nodes.package missing: %v", err)
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

func TestMigrateV4ToV5DropRebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ckg.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`
		CREATE TABLE files (
			id INTEGER PRIMARY KEY,
			path TEXT UNIQUE NOT NULL,
			hash TEXT NOT NULL,
			language TEXT NOT NULL,
			updated_at DATETIME NOT NULL
		);
		CREATE TABLE nodes (
			id INTEGER PRIMARY KEY,
			file_id INTEGER NOT NULL,
			fqn TEXT UNIQUE NOT NULL,
			short_name TEXT NOT NULL,
			kind TEXT NOT NULL,
			line_start INTEGER NOT NULL,
			line_end INTEGER NOT NULL,
			complexity INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE edges (
			id INTEGER PRIMARY KEY,
			source_id INTEGER NOT NULL,
			target_id INTEGER,
			target_fqn TEXT NOT NULL,
			relation TEXT NOT NULL
		);
	`); err != nil {
		db.Close()
		t.Fatalf("seed v4 schema: %v", err)
	}
	if _, err = db.Exec(`INSERT INTO files (path, hash, language, updated_at) VALUES ('legacy.go', 'h', 'go', datetime('now'))`); err != nil {
		db.Close()
		t.Fatalf("seed v4 row: %v", err)
	}
	if _, err = db.Exec(`PRAGMA user_version = 4`); err != nil {
		db.Close()
		t.Fatalf("seed v4 version: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore migrate v4→v5: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	var v int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != 5 {
		t.Fatalf("user_version = %d, want 5", v)
	}
	var files int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM files`).Scan(&files); err != nil {
		t.Fatal(err)
	}
	if files != 0 {
		t.Fatalf("v4 rows should be dropped, files=%d", files)
	}
	for _, idx := range []string{"idx_nodes_package", "idx_edges_source_rel", "idx_edges_target_rel"} {
		var name string
		if err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, idx).Scan(&name); err != nil {
			t.Fatalf("index %s missing after v5 rebuild: %v", idx, err)
		}
	}
}
