package ckg

import (
	"context"
	"database/sql"
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
	Name       string
	Type       string // func, struct, interface, method
	LineStart  int
	LineEnd    int
	Complexity int
}

type Edge struct {
	SourceName string
	TargetName string
	Relation   string // calls, implements, instantiates
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
	ddl := `
	CREATE TABLE IF NOT EXISTS files (
		id INTEGER PRIMARY KEY,
		path TEXT UNIQUE NOT NULL,
		hash TEXT NOT NULL,
		language TEXT NOT NULL,
		updated_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS nodes (
		id INTEGER PRIMARY KEY,
		file_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		type TEXT NOT NULL,
		line_start INTEGER NOT NULL,
		line_end INTEGER NOT NULL,
		complexity INTEGER NOT NULL DEFAULT 0,
		FOREIGN KEY(file_id) REFERENCES files(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS edges (
		file_id INTEGER NOT NULL,
		source_name TEXT NOT NULL,
		target_name TEXT NOT NULL,
		relation TEXT NOT NULL,
		FOREIGN KEY(file_id) REFERENCES files(id) ON DELETE CASCADE,
		UNIQUE(file_id, source_name, target_name, relation)
	);
	
	CREATE INDEX IF NOT EXISTS idx_files_path ON files(path);
	CREATE INDEX IF NOT EXISTS idx_nodes_name ON nodes(name);
	`
	_, err := s.db.Exec(ddl)
	return err
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

// SaveFileNodes saves a file, its nodes, and its edges in a single atomic transaction.
// The edges slice should contain edges where source or target points to nodes within this file.
// Note: Inserting cross-file edges might require nodes to exist first.
func (s *Store) SaveFileNodes(ctx context.Context, path string, hash string, lang string, nodes []Node, edges []Edge) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	// Defer rollback to ensure we don't leak transactions. It is a no-op if tx is already committed.
	defer tx.Rollback()

	// 1. Delete existing file record if any (cascades to nodes and edges)
	_, err = tx.ExecContext(ctx, "DELETE FROM files WHERE path = ?", path)
	if err != nil {
		return err
	}

	// 2. Insert new file record
	res, err := tx.ExecContext(ctx, "INSERT INTO files (path, hash, language, updated_at) VALUES (?, ?, ?, ?)", path, hash, lang, time.Now())
	if err != nil {
		return err
	}
	fileID, err := res.LastInsertId()
	if err != nil {
		return err
	}

	// 3. Insert nodes
	// We need to keep a mapping from original Node.ID (if it has one) to the new DB ID,
	// or we can assign IDs manually or fetch them. For the local graph, usually
	// nodes are parsed fresh and don't have IDs yet. We will assign new IDs.
	// If edges connect to these newly created nodes, we need their IDs.
	// We'll update the Node structs in-place with the inserted IDs.
	if len(nodes) > 0 {
		stmt, err := tx.PrepareContext(ctx, "INSERT INTO nodes (file_id, name, type, line_start, line_end, complexity) VALUES (?, ?, ?, ?, ?, ?) RETURNING id")
		if err != nil {
			return err
		}
		defer stmt.Close()

		for i := range nodes {
			var newID int64
			err = stmt.QueryRowContext(ctx, fileID, nodes[i].Name, nodes[i].Type, nodes[i].LineStart, nodes[i].LineEnd, nodes[i].Complexity).Scan(&newID)
			if err != nil {
				return err
			}
			nodes[i].ID = newID
		}
	}

	// 4. Insert edges
	if len(edges) > 0 {
		stmt, err := tx.PrepareContext(ctx, "INSERT OR IGNORE INTO edges (file_id, source_name, target_name, relation) VALUES (?, ?, ?, ?)")
		if err != nil {
			return err
		}
		defer stmt.Close()

		for _, e := range edges {
			_, err = stmt.ExecContext(ctx, fileID, e.SourceName, e.TargetName, e.Relation)
			if err != nil {
				return err
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
