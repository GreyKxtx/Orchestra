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

	const targetVersion = 2 // bump for each new schema version

	if version > targetVersion {
		return fmt.Errorf("ckg store: database schema version %d is newer than supported %d; upgrade the binary", version, targetVersion)
	}

	if version >= targetVersion {
		return nil
	}

	// Local cache — drop+recreate is acceptable. Incremental scan rebuilds quickly.
	drop := `
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
    `
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("migrate v2: begin tx: %w", err)
	}
	if _, err := tx.Exec(drop); err != nil {
		tx.Rollback()
		return fmt.Errorf("migrate v2: drop old: %w", err)
	}
	if _, err := tx.Exec(ddl); err != nil {
		tx.Rollback()
		return fmt.Errorf("migrate v2: apply schema: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migrate v2: commit: %w", err)
	}
	if _, err := s.db.Exec("PRAGMA user_version = 2"); err != nil {
		return fmt.Errorf("migrate v2: set user_version: %w", err)
	}
	return nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

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
			var targetID *int64
			var tid int64
			err := sel.QueryRowContext(ctx, e.TargetFQN).Scan(&tid)
			if err == nil {
				targetID = &tid
			} else if err != sql.ErrNoRows {
				return fmt.Errorf("resolve target %s: %w", e.TargetFQN, err)
			}
			if _, err := ins.ExecContext(ctx, sourceID, targetID, e.TargetFQN, e.Relation); err != nil {
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
	}

	return tx.Commit()
}

// DeleteFile deletes a file and cascades its deletion to nodes and edges.
func (s *Store) DeleteFile(ctx context.Context, path string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM files WHERE path = ?", path)
	return err
}
