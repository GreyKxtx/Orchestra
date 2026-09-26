package ckg

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// StagedFile is a file as a turn has it before its edits are applied: the
// content staged for a slash-separated path under the root.
type StagedFile struct {
	Path    string
	Content []byte
}

// overlayScope is what a context carries while the graph answers from a
// turn's staged files: the connection whose temp schema shadows the graph's
// tables, and the reader that serves the staged content for snippets.
type overlayScope struct {
	conn *sql.Conn
	read func(rel string) ([]byte, bool)
}

type overlayKey struct{}

// q is where a read goes: the overlay connection the context carries, else
// the store's pool.
func (s *Store) q(ctx context.Context) queryer {
	if sc, ok := ctx.Value(overlayKey{}).(*overlayScope); ok && sc != nil && sc.conn != nil {
		return sc.conn
	}
	return s.db
}

// overlayRead is the staged content of rel when the context carries an
// overlay that has it.
func overlayRead(ctx context.Context, rel string) ([]byte, bool) {
	sc, ok := ctx.Value(overlayKey{}).(*overlayScope)
	if !ok || sc == nil || sc.read == nil {
		return nil, false
	}
	return sc.read(rel)
}

// The overlay's schema. SQLite resolves an unqualified table name in the
// temp schema before main, so views named files, nodes and edges stand in
// for the graph's tables on the one connection that has them: the graph
// without the replaced files' rows, plus the staged files' own. An edge from
// the graph into a replaced file points at the staged node of the same FQN.
const overlayDDL = `
CREATE TEMP TABLE ov_files (
    id INTEGER PRIMARY KEY, path TEXT NOT NULL, hash TEXT NOT NULL, language TEXT NOT NULL,
    module_path TEXT, package TEXT, updated_at DATETIME NOT NULL,
    mtime_ns INTEGER NOT NULL DEFAULT 0, size INTEGER NOT NULL DEFAULT -1);
CREATE TEMP TABLE ov_nodes (
    id INTEGER PRIMARY KEY, file_id INTEGER NOT NULL, fqn TEXT NOT NULL, short_name TEXT NOT NULL,
    kind TEXT NOT NULL, line_start INTEGER NOT NULL, line_end INTEGER NOT NULL,
    complexity INTEGER NOT NULL DEFAULT 0, package TEXT NOT NULL DEFAULT '');
CREATE INDEX temp.ov_nodes_fqn ON ov_nodes(fqn);
CREATE INDEX temp.ov_nodes_short ON ov_nodes(short_name);
CREATE INDEX temp.ov_nodes_pkg ON ov_nodes(package, short_name);
CREATE TEMP TABLE ov_edges (
    id INTEGER PRIMARY KEY, source_id INTEGER NOT NULL, target_id INTEGER, target_fqn TEXT NOT NULL,
    relation TEXT NOT NULL, is_external INTEGER NOT NULL DEFAULT 0);
CREATE INDEX temp.ov_edges_src ON ov_edges(source_id);
CREATE INDEX temp.ov_edges_tgt ON ov_edges(target_id);
CREATE INDEX temp.ov_edges_tfqn ON ov_edges(target_fqn);
CREATE UNIQUE INDEX temp.ov_edges_unique ON ov_edges(source_id, target_fqn, relation);
CREATE TEMP TABLE ov_replaced_files (id INTEGER PRIMARY KEY);
CREATE TEMP TABLE ov_replaced_nodes (id INTEGER PRIMARY KEY);
CREATE TEMP VIEW files AS
  SELECT id, path, hash, language, module_path, package, updated_at, mtime_ns, size FROM main.files
   WHERE id NOT IN (SELECT id FROM temp.ov_replaced_files)
  UNION ALL
  SELECT id, path, hash, language, module_path, package, updated_at, mtime_ns, size FROM temp.ov_files;
CREATE TEMP VIEW nodes AS
  SELECT id, file_id, fqn, short_name, kind, line_start, line_end, complexity, package FROM main.nodes
   WHERE file_id NOT IN (SELECT id FROM temp.ov_replaced_files)
  UNION ALL
  SELECT id, file_id, fqn, short_name, kind, line_start, line_end, complexity, package FROM temp.ov_nodes;
CREATE TEMP VIEW edges AS
  SELECT e.id, e.source_id,
         CASE WHEN e.target_id IN (SELECT id FROM temp.ov_replaced_nodes)
              THEN (SELECT o.id FROM temp.ov_nodes o WHERE o.fqn = e.target_fqn LIMIT 1)
              ELSE e.target_id END AS target_id,
         e.target_fqn, e.relation, e.is_external
    FROM main.edges e
   WHERE e.source_id NOT IN (SELECT id FROM temp.ov_replaced_nodes)
  UNION ALL
  SELECT id, source_id, target_id, target_fqn, relation, is_external FROM temp.ov_edges;
`

const overlayDropDDL = `
DROP VIEW IF EXISTS temp.edges;
DROP VIEW IF EXISTS temp.nodes;
DROP VIEW IF EXISTS temp.files;
DROP TABLE IF EXISTS temp.ov_edges;
DROP TABLE IF EXISTS temp.ov_nodes;
DROP TABLE IF EXISTS temp.ov_files;
DROP TABLE IF EXISTS temp.ov_replaced_files;
DROP TABLE IF EXISTS temp.ov_replaced_nodes;
`

// OverlayContext gives ctx a view of the graph in which the staged files
// stand for their paths (LLM-11): the graph used to answer explore from the
// disk while the model's edits sat in the turn's overlay, so a symbol the
// model had just written was unknown and the one it had just removed was
// still there. Every read under the returned context goes through a
// connection of its own whose temp schema shadows the graph's tables;
// nothing is written to the graph, and no other connection sees the staged
// rows. read serves the staged content for the snippets the answers quote.
// release ends it: the temp schema is dropped before the connection returns
// to the pool, since a pooled connection that kept the views would shadow
// the graph for whoever got it next. A file the parser does not know, or
// cannot parse, stays as the graph has it.
//
// The store is one connection (NewStore), and the overlay holds it until
// release: a read under the returned context goes through it, a read with
// any other context waits for it. Answer the whole question under the one
// context, then release; never read the disk's graph beside a held overlay
// on the same goroutine.
func (o *Orchestrator) OverlayContext(ctx context.Context, files []StagedFile, read func(rel string) ([]byte, bool)) (context.Context, func(), error) {
	none := func() {}
	if o == nil || o.store == nil || o.store.db == nil {
		return ctx, none, nil
	}
	var indexable []StagedFile
	for _, f := range files {
		if SitterLanguageFor(strings.ToLower(filepath.Ext(f.Path))) != nil {
			indexable = append(indexable, f)
		}
	}
	if len(indexable) == 0 {
		return ctx, none, nil
	}
	conn, err := o.store.db.Conn(ctx)
	if err != nil {
		return ctx, none, fmt.Errorf("graph overlay: %w", err)
	}
	release := func() {
		_, _ = conn.ExecContext(context.Background(), overlayDropDDL)
		_ = conn.Close()
	}
	if err := o.buildOverlay(ctx, conn, indexable); err != nil {
		release()
		return ctx, none, fmt.Errorf("graph overlay: %w", err)
	}
	return context.WithValue(ctx, overlayKey{}, &overlayScope{conn: conn, read: read}), release, nil
}

type parsedStaged struct {
	fileID  int64
	pkgName string
	module  string
	nodes   []Node
	edges   []Edge
}

// buildOverlay fills the temp schema on conn: the staged files parsed, their
// paths' rows in the graph set aside, their edges resolved against the
// graph as the views now show it. Rows get negative ids so they never
// collide with the graph's.
func (o *Orchestrator) buildOverlay(ctx context.Context, conn *sql.Conn, files []StagedFile) error {
	if _, err := conn.ExecContext(ctx, overlayDDL); err != nil {
		return err
	}
	var parsed []parsedStaged
	for i, f := range files {
		rel := strings.TrimPrefix(filepath.ToSlash(f.Path), "./")
		ext := strings.ToLower(filepath.Ext(rel))
		mp := o.modulePathFor(ext)
		nodes, edges, pkgName, err := ParseSource(ctx, mp, o.root, filepath.Join(o.root, filepath.FromSlash(rel)), f.Content)
		if err != nil {
			continue
		}
		if _, err := conn.ExecContext(ctx, `INSERT OR IGNORE INTO temp.ov_replaced_files SELECT id FROM main.files WHERE path = ?`, rel); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, `INSERT OR IGNORE INTO temp.ov_replaced_nodes SELECT id FROM main.nodes WHERE file_id IN (SELECT id FROM main.files WHERE path = ?)`, rel); err != nil {
			return err
		}
		fileID := int64(-(i + 1))
		if _, err := conn.ExecContext(ctx,
			`INSERT INTO temp.ov_files (id, path, hash, language, module_path, package, updated_at, mtime_ns, size) VALUES (?, ?, 'staged', ?, ?, ?, ?, 0, ?)`,
			fileID, rel, LanguageFromExt(ext), mp, pkgName, time.Now(), len(f.Content)); err != nil {
			return err
		}
		parsed = append(parsed, parsedStaged{fileID: fileID, pkgName: pkgName, module: mp, nodes: nodes, edges: edges})
	}
	// Every file's nodes first, then the edges, which resolve against them.
	ids := make([]map[string]int64, len(parsed))
	nextID := int64(-1)
	for i, p := range parsed {
		ids[i] = make(map[string]int64, len(p.nodes))
		for _, n := range p.nodes {
			// A node another file declares too (a package's) stays that
			// file's row: the graph keeps one row per FQN.
			var existing int64
			err := conn.QueryRowContext(ctx, `SELECT id FROM nodes WHERE fqn = ? LIMIT 1`, n.FQN).Scan(&existing)
			if err == nil {
				ids[i][n.FQN] = existing
				continue
			}
			if err != sql.ErrNoRows {
				return err
			}
			pkg := n.Package
			if pkg == "" {
				pkg = p.pkgName
			}
			if _, err := conn.ExecContext(ctx,
				`INSERT INTO temp.ov_nodes (id, file_id, fqn, short_name, kind, line_start, line_end, complexity, package) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				nextID, p.fileID, n.FQN, n.ShortName, n.Kind, n.LineStart, n.LineEnd, n.Complexity, pkg); err != nil {
				return err
			}
			ids[i][n.FQN] = nextID
			nextID--
		}
	}
	for i, p := range parsed {
		for _, e := range p.edges {
			sourceID, ok := ids[i][e.SourceFQN]
			if !ok {
				continue
			}
			targetID, resolved, err := resolveEdgeTarget(ctx, conn, nil, e.TargetFQN, e.Relation, p.pkgName)
			if err != nil {
				return err
			}
			targetFQN := e.TargetFQN
			if resolved != "" {
				targetFQN = resolved
			}
			ext := 0
			if e.IsExternal || (targetID == nil && isExternalTarget(targetFQN, p.module)) {
				ext = 1
			}
			if _, err := conn.ExecContext(ctx,
				`INSERT OR IGNORE INTO temp.ov_edges (source_id, target_id, target_fqn, relation, is_external) VALUES (?, ?, ?, ?, ?)`,
				sourceID, targetID, targetFQN, e.Relation, ext); err != nil {
				return err
			}
		}
	}
	return nil
}
