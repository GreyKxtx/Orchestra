package ckg

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type Node struct {
	ID         int64
	FileID     int64
	FQN        string // "github.com/x/y/internal/agent.Agent.Run"
	ShortName  string // "Run" or "Agent.Run" for methods
	Kind       string // "func" | "method" | "struct" | "interface" | "type" | "package"
	LineStart  int
	LineEnd    int
	Complexity int
}

// Edge represents a directed relation in the graph.
// SourceFQN must reference a node within the same file as it's saved (resolved
// inside SaveFileNodes). TargetFQN may reference any node (internal or external);
// the resolver fills in TargetID where possible.
type Edge struct {
	SourceFQN string
	TargetFQN string
	Relation  string // "calls" | "imports" | "instantiates"
}

// NewStore initializes the SQLite database with the given path.
func NewStore(dbPath string) (*Store, error) {
	// Enable foreign keys via connection string for modernc.org/sqlite
	sep := "?"
	if strings.Contains(dbPath, "?") {
		sep = "&"
	}
	db, err := sql.Open("sqlite", dbPath+sep+"_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("migrate: read user_version: %w", err)
	}

	const targetVersion = 3 // bump for each new schema version

	if version > targetVersion {
		return fmt.Errorf("ckg store: database schema version %d is newer than supported %d; upgrade the binary", version, targetVersion)
	}

	if version >= targetVersion {
		return nil
	}

	// Local cache — drop+recreate is acceptable. Incremental scan rebuilds quickly.
	drop := `
        DROP TABLE IF EXISTS spans;
        DROP TABLE IF EXISTS traces;
        DROP TABLE IF EXISTS edges;
        DROP TABLE IF EXISTS nodes;
        DROP TABLE IF EXISTS files;
    `
	ddl := `
    CREATE TABLE files (
        id          INTEGER PRIMARY KEY,
        path        TEXT UNIQUE NOT NULL,
        hash        TEXT NOT NULL,
        language    TEXT NOT NULL,
        module_path TEXT,
        package     TEXT,
        updated_at  DATETIME NOT NULL
    );
    CREATE INDEX idx_files_path ON files(path);

    CREATE TABLE nodes (
        id          INTEGER PRIMARY KEY,
        file_id     INTEGER NOT NULL,
        fqn         TEXT UNIQUE NOT NULL,
        short_name  TEXT NOT NULL,
        kind        TEXT NOT NULL,
        line_start  INTEGER NOT NULL,
        line_end    INTEGER NOT NULL,
        complexity  INTEGER NOT NULL DEFAULT 0,
        FOREIGN KEY(file_id) REFERENCES files(id) ON DELETE CASCADE
    );
    CREATE INDEX idx_nodes_fqn        ON nodes(fqn);
    CREATE INDEX idx_nodes_short_name ON nodes(short_name);
    CREATE INDEX idx_nodes_file_id    ON nodes(file_id);

    CREATE TABLE edges (
        id         INTEGER PRIMARY KEY,
        source_id  INTEGER NOT NULL,
        target_id  INTEGER,
        target_fqn TEXT NOT NULL,
        relation   TEXT NOT NULL,
        FOREIGN KEY(source_id) REFERENCES nodes(id) ON DELETE CASCADE,
        FOREIGN KEY(target_id) REFERENCES nodes(id) ON DELETE SET NULL
    );
    CREATE INDEX idx_edges_source_id  ON edges(source_id);
    CREATE INDEX idx_edges_target_id  ON edges(target_id);
    CREATE INDEX idx_edges_target_fqn ON edges(target_fqn);
    CREATE UNIQUE INDEX idx_edges_unique ON edges(source_id, target_fqn, relation);

    CREATE TABLE traces (
        id          TEXT PRIMARY KEY,
        service     TEXT,
        started_at  DATETIME,
        duration_ms INTEGER
    );

    CREATE TABLE spans (
        id             INTEGER PRIMARY KEY AUTOINCREMENT,
        span_id        TEXT NOT NULL,
        trace_id       TEXT NOT NULL,
        parent_span_id TEXT,
        name           TEXT NOT NULL,
        service        TEXT,
        code_file      TEXT,
        code_lineno    INTEGER,
        code_func      TEXT,
        ckg_node_id    INTEGER,
        resolve_status TEXT,
        started_at     DATETIME,
        duration_ms    INTEGER,
        status         TEXT,
        error_msg      TEXT,
        attributes     TEXT,
        FOREIGN KEY(trace_id)    REFERENCES traces(id),
        FOREIGN KEY(ckg_node_id) REFERENCES nodes(id) ON DELETE SET NULL,
        UNIQUE(trace_id, span_id)
    );
    CREATE INDEX idx_spans_trace_id    ON spans(trace_id);
    CREATE INDEX idx_spans_ckg_node_id ON spans(ckg_node_id);
    CREATE INDEX idx_spans_code_file   ON spans(code_file);
    `
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("migrate v3: begin tx: %w", err)
	}
	if _, err := tx.Exec(drop); err != nil {
		tx.Rollback()
		return fmt.Errorf("migrate v3: drop old: %w", err)
	}
	if _, err := tx.Exec(ddl); err != nil {
		tx.Rollback()
		return fmt.Errorf("migrate v3: apply schema: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migrate v3: commit: %w", err)
	}
	if _, err := s.db.Exec("PRAGMA user_version = 3"); err != nil {
		return fmt.Errorf("migrate v3: set user_version: %w", err)
	}
	return nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying *sql.DB for advanced queries (e.g. runtime.query tool).
func (s *Store) DB() *sql.DB { return s.db }

// GetFileHash returns the hash of the file if it exists, otherwise an empty string.
func (s *Store) GetFileHash(ctx context.Context, path string) (string, error) {
	var hash string
	err := s.db.QueryRowContext(ctx, "SELECT hash FROM files WHERE path = ?", path).Scan(&hash)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return hash, nil
}

// GetAllFiles returns a map of path -> hash for all files currently in the database.
func (s *Store) GetAllFiles(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT path, hash FROM files")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	m := make(map[string]string)
	for rows.Next() {
		var path, hash string
		if err := rows.Scan(&path, &hash); err != nil {
			return nil, err
		}
		m[path] = hash
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return m, nil
}

// SaveFileNodes upserts a single file's nodes/edges atomically.
//
// Edges may target symbols not yet indexed; their target_id will be NULL until
// the matching FQN is indexed (then a follow-up UPDATE in this same call
// resolves any previously-NULL edges whose target_fqn matches a freshly-inserted
// node — see step 5 below).
func (s *Store) SaveFileNodes(ctx context.Context, path, hash, lang, modulePath, pkgName string, nodes []Node, edges []Edge) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("save file nodes: begin tx: %w", err)
	}
	defer tx.Rollback()

	// 1. Cascading delete of the old file row clears its nodes and edges.
	if _, err := tx.ExecContext(ctx, "DELETE FROM files WHERE path = ?", path); err != nil {
		return fmt.Errorf("delete old file: %w", err)
	}

	// 2. Insert new files row.
	res, err := tx.ExecContext(ctx,
		`INSERT INTO files (path, hash, language, module_path, package, updated_at)
         VALUES (?, ?, ?, ?, ?, ?)`,
		path, hash, lang, modulePath, pkgName, time.Now())
	if err != nil {
		return fmt.Errorf("insert file: %w", err)
	}
	fileID, err := res.LastInsertId()
	if err != nil {
		return err
	}

	// 3. Insert nodes. Use ON CONFLICT(fqn) DO UPDATE to handle duplicates
	//    (e.g. two files in the same package both produce the same package-FQN node).
	fqnToID := make(map[string]int64, len(nodes))
	if len(nodes) > 0 {
		stmt, err := tx.PrepareContext(ctx,
			`INSERT INTO nodes (file_id, fqn, short_name, kind, line_start, line_end, complexity)
             VALUES (?, ?, ?, ?, ?, ?, ?)
             ON CONFLICT(fqn) DO UPDATE SET
                 file_id    = excluded.file_id,
                 short_name = excluded.short_name,
                 kind       = excluded.kind,
                 line_start = excluded.line_start,
                 line_end   = excluded.line_end,
                 complexity = excluded.complexity
             RETURNING id`)
		if err != nil {
			return fmt.Errorf("prepare insert node: %w", err)
		}
		defer stmt.Close()

		for i := range nodes {
			var newID int64
			err := stmt.QueryRowContext(ctx,
				fileID, nodes[i].FQN, nodes[i].ShortName, nodes[i].Kind,
				nodes[i].LineStart, nodes[i].LineEnd, nodes[i].Complexity,
			).Scan(&newID)
			if err != nil {
				return fmt.Errorf("insert node %s: %w", nodes[i].FQN, err)
			}
			nodes[i].ID = newID
			fqnToID[nodes[i].FQN] = newID
		}
	}

	// 4. Insert edges. SourceFQN must match a node we just inserted (in this file).
	//    TargetFQN may resolve to any node (same file, other file, or external/unknown).
	if len(edges) > 0 {
		ins, err := tx.PrepareContext(ctx,
			`INSERT OR IGNORE INTO edges (source_id, target_id, target_fqn, relation)
             VALUES (?, ?, ?, ?)`)
		if err != nil {
			return fmt.Errorf("prepare insert edge: %w", err)
		}
		defer ins.Close()

		sel, err := tx.PrepareContext(ctx, `SELECT id FROM nodes WHERE fqn = ?`)
		if err != nil {
			return fmt.Errorf("prepare select target: %w", err)
		}
		defer sel.Close()

		for _, e := range edges {
			sourceID, ok := fqnToID[e.SourceFQN]
			if !ok {
				// Source was not in our nodes — likely a parser bug; skip silently.
				continue
			}
			targetID, resolvedTargetFQN, err := resolveEdgeTarget(ctx, tx, sel, e.TargetFQN, e.Relation)
			if err != nil {
				return err
			}
			targetFQN := e.TargetFQN
			if resolvedTargetFQN != "" {
				targetFQN = resolvedTargetFQN
			}
			if _, err := ins.ExecContext(ctx, sourceID, targetID, targetFQN, e.Relation); err != nil {
				return fmt.Errorf("insert edge %s→%s: %w", e.SourceFQN, e.TargetFQN, err)
			}
		}
	}

	// 5. Lazy-resolve previously-dangling edges: any edges whose target_fqn matches
	//    a node we just inserted should now have target_id pointed at it.
	if len(fqnToID) > 0 {
		upd, err := tx.PrepareContext(ctx,
			`UPDATE edges SET target_id = ? WHERE target_fqn = ? AND target_id IS NULL`)
		if err != nil {
			return fmt.Errorf("prepare lazy resolve: %w", err)
		}
		defer upd.Close()
		for fqn, id := range fqnToID {
			if _, err := upd.ExecContext(ctx, id, fqn); err != nil {
				return fmt.Errorf("lazy resolve %s: %w", fqn, err)
			}
		}

		// Also resolve dangling call edges that still carry short names.
		// We update only when the short name is globally unique at update time.
		updShort, err := tx.PrepareContext(ctx, `
			UPDATE edges
			SET target_id = ?, target_fqn = ?
			WHERE target_id IS NULL
			  AND relation IN ('calls', 'instantiates')
			  AND target_fqn = ?
			  AND NOT EXISTS (
				  SELECT 1 FROM nodes n2
				  WHERE n2.short_name = ? AND n2.id <> ?
			  )`)
		if err != nil {
			return fmt.Errorf("prepare lazy resolve by short_name: %w", err)
		}
		defer updShort.Close()

		for _, n := range nodes {
			if n.ShortName == "" {
				continue
			}
			if _, err := updShort.ExecContext(ctx, n.ID, n.FQN, n.ShortName, n.ShortName, n.ID); err != nil {
				return fmt.Errorf("lazy resolve short_name %s: %w", n.ShortName, err)
			}
		}
	}

	return tx.Commit()
}

func resolveEdgeTarget(ctx context.Context, tx *sql.Tx, selByFQN *sql.Stmt, rawTarget, relation string) (*int64, string, error) {
	var tid int64
	err := selByFQN.QueryRowContext(ctx, rawTarget).Scan(&tid)
	if err == nil {
		return &tid, rawTarget, nil
	}
	if err != sql.ErrNoRows {
		return nil, "", fmt.Errorf("resolve target %s: %w", rawTarget, err)
	}

	// Parser currently emits short names for calls (known limitation). If a short name
	// maps to exactly one node in the graph, resolve it and canonicalize target_fqn.
	if relation != "calls" && relation != "instantiates" {
		return nil, rawTarget, nil
	}
	if strings.Contains(rawTarget, "/") {
		return nil, rawTarget, nil
	}

	rows, err := tx.QueryContext(ctx, `SELECT id, fqn FROM nodes WHERE short_name = ? LIMIT 2`, rawTarget)
	if err != nil {
		return nil, "", fmt.Errorf("resolve short_name %s: %w", rawTarget, err)
	}
	defer rows.Close()

	type candidate struct {
		id  int64
		fqn string
	}
	var cands []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.fqn); err != nil {
			return nil, "", fmt.Errorf("scan short_name candidate %s: %w", rawTarget, err)
		}
		cands = append(cands, c)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate short_name candidates %s: %w", rawTarget, err)
	}
	if len(cands) == 1 {
		return &cands[0].id, cands[0].fqn, nil
	}
	return nil, rawTarget, nil
}

// DeleteFile deletes a file and cascades its deletion to nodes and edges.
func (s *Store) DeleteFile(ctx context.Context, path string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM files WHERE path = ?", path)
	return err
}

// FindNodeAtLine returns the innermost CKG node containing the given 1-based line
// in the file identified by its CKG-canonical slash-relative path.
// Returns (0, nil) when no node matches.
func (s *Store) FindNodeAtLine(ctx context.Context, filePath string, lineno int) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
		SELECT n.id
		FROM nodes n
		JOIN files f ON f.id = n.file_id
		WHERE f.path = ? AND n.line_start <= ? AND n.line_end >= ?
		ORDER BY n.line_start DESC
		LIMIT 1`, filePath, lineno, lineno).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return id, nil
}
