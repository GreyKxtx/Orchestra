package ckg

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
	// indexMu holds readers (traversals, stats, step-1 context) out while a
	// refresh writes the graph. Do not lock it inside SaveFileNodes /
	// GetAllFiles — the orchestrator holds it around them.
	indexMu sync.RWMutex
	// refreshMu serializes refreshes with each other. A refresh scans and
	// parses under it alone, and takes indexMu only to write, so readers
	// wait for the writes and not for the walk (DATA-3).
	refreshMu sync.Mutex
	// refreshing is set while RefreshInBackground has a pass in flight, so
	// concurrent callers do not queue up passes behind it.
	refreshing atomic.Bool

	statsMu     sync.Mutex
	lastRefresh RefreshStats
	// refreshHooks run after a pass that changed the graph, outside its
	// locks: the embeddings pass re-indexes what changed (DATA-5).
	hooksMu      sync.Mutex
	refreshHooks []func(RefreshStats)

	// The semantic index: the model's vectors in memory, normalized, rebuilt
	// when embedGen moves (a vector saved or cleared, a file's nodes
	// rewritten). embedLoads counts the rebuilds, for tests.
	embedGen   atomic.Uint64
	embedMu    sync.Mutex
	embedIdx   *embedIndex
	embedLoads atomic.Int64
}

// OnRefresh registers fn to run after every pass that changed the graph
// (parsed or deleted a file), once the pass has released its locks.
func (s *Store) OnRefresh(fn func(RefreshStats)) {
	if s == nil || fn == nil {
		return
	}
	s.hooksMu.Lock()
	s.refreshHooks = append(s.refreshHooks, fn)
	s.hooksMu.Unlock()
}

func (s *Store) fireRefresh(st RefreshStats) {
	s.hooksMu.Lock()
	hooks := make([]func(RefreshStats), len(s.refreshHooks))
	copy(hooks, s.refreshHooks)
	s.hooksMu.Unlock()
	for _, fn := range hooks {
		fn(st)
	}
}

// RefreshStats describes the last UpdateGraph pass.
type RefreshStats struct {
	// Seen is how many indexable files the walk found.
	Seen int
	// Hashed is how many of them had to be read and hashed: their stamp
	// (mtime, size) was unknown or had changed.
	Hashed int
	// Parsed and Deleted are the files whose graph rows changed.
	Parsed  int
	Deleted int
	// Candidates is how many dangling edges the pass looked at for
	// relinking, Relinked how many it resolved. Both are 0 on an empty pass.
	Candidates int
	Relinked   int
	// Elapsed is the whole pass; Locked is how long readers were held out.
	Elapsed time.Duration
	Locked  time.Duration
}

// LastRefresh is what the last UpdateGraph pass did.
func (s *Store) LastRefresh() RefreshStats {
	if s == nil {
		return RefreshStats{}
	}
	s.statsMu.Lock()
	defer s.statsMu.Unlock()
	return s.lastRefresh
}

func (s *Store) setLastRefresh(st RefreshStats) {
	s.statsMu.Lock()
	s.lastRefresh = st
	s.statsMu.Unlock()
}

// FileStamp is what the scanner keeps about an indexed file: the content
// hash the graph was built from, and the mtime and size it had, which say
// whether the file has to be read again (DATA-3: every pass hashed every
// file).
type FileStamp struct {
	Hash    string
	MTimeNS int64
	Size    int64
}

type Node struct {
	ID         int64
	FileID     int64
	FQN        string // "github.com/x/y/internal/agent.Agent.Run"
	ShortName  string // "Run" or "Agent.Run" for methods
	Kind       string // "func" | "method" | "struct" | "interface" | "type" | "package" | "external"
	LineStart  int
	LineEnd    int
	Complexity int
	Package    string // package/module short or import path; indexed for FQN linking
	RelPath    string // file path; filled by traversal queries, not a DB column
}

// Edge represents a directed relation in the graph.
// SourceFQN must reference a node within the same file as it's saved (resolved
// inside SaveFileNodes). TargetFQN may reference any node (internal or external);
// the resolver fills in TargetID where possible.
type Edge struct {
	SourceFQN  string
	TargetFQN  string
	Relation   string // "calls" | "imports" | "instantiates"
	IsExternal bool   // stdlib / other-module; no node row is created
}

// NewStore initializes the SQLite database with the given path.
func NewStore(dbPath string) (*Store, error) {
	// Enable foreign keys via connection string for modernc.org/sqlite
	sep := "?"
	if strings.Contains(dbPath, "?") {
		sep = "&"
	}
	// WAL lets the TUI, the extension and ckg-ui read while a refresh
	// writes, instead of queueing on the rollback journal's lock; NORMAL
	// syncs the WAL at checkpoints rather than at every file's commit — the
	// graph is a cache rebuilt from the tree, a lost last transaction costs a
	// rescan. A memory database ignores both. (DATA-7)
	db, err := sql.Open("sqlite", dbPath+sep+"_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	// One connection: SQLite writers and the in-process RWMutex stay aligned.
	// Concurrent TraverseBFS calls queue here instead of hitting SQLITE_BUSY.
	db.SetMaxOpenConns(1)
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

	const targetVersion = 5 // bump for each new schema version

	if version > targetVersion {
		return fmt.Errorf("ckg store: database schema version %d is newer than supported %d; upgrade the binary", version, targetVersion)
	}

	// memory_embeddings is created outside the versioned block, and never
	// dropped by it, because it is not part of the code graph. The graph is a
	// local cache whose rebuild costs only CPU; memory vectors cost real calls
	// to the embedding endpoint, and their key is the chunk's content hash, so
	// nothing about a graph rebuild invalidates them. See memory_embed_store.go.
	if err := s.ensureMemoryEmbeddings(); err != nil {
		return err
	}

	// The embedding cache is keyed by content, not by node id, so it is
	// created beside the versioned schema and never dropped by it: a graph
	// rebuild renumbers every node, and the vectors cost real calls.
	if err := s.ensureEmbeddingCache(); err != nil {
		return err
	}

	if version >= targetVersion {
		if err := s.ensureFileStamps(); err != nil {
			return err
		}
		return s.ensureEmbeddingHashColumn()
	}

	// Local cache: any older user_version (including v4 without package /
	// is_external / the v5 indexes) is drop+recreate. Incremental scan rebuilds
	// the graph on the next UpdateGraph; do not try in-place ALTER.
	drop := `
        DROP TABLE IF EXISTS node_embeddings;
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
        updated_at  DATETIME NOT NULL,
        mtime_ns    INTEGER NOT NULL DEFAULT 0,
        size        INTEGER NOT NULL DEFAULT -1
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
        package     TEXT NOT NULL DEFAULT '',
        FOREIGN KEY(file_id) REFERENCES files(id) ON DELETE CASCADE
    );
    CREATE INDEX idx_nodes_fqn        ON nodes(fqn);
    CREATE INDEX idx_nodes_short_name ON nodes(short_name);
    CREATE INDEX idx_nodes_file_id    ON nodes(file_id);
    CREATE INDEX idx_nodes_package    ON nodes(package, short_name);

    CREATE TABLE edges (
        id          INTEGER PRIMARY KEY,
        source_id   INTEGER NOT NULL,
        target_id   INTEGER,
        target_fqn  TEXT NOT NULL,
        relation    TEXT NOT NULL,
        is_external INTEGER NOT NULL DEFAULT 0,
        FOREIGN KEY(source_id) REFERENCES nodes(id) ON DELETE CASCADE,
        FOREIGN KEY(target_id) REFERENCES nodes(id) ON DELETE SET NULL
    );
    CREATE INDEX idx_edges_source_id  ON edges(source_id);
    CREATE INDEX idx_edges_target_id  ON edges(target_id);
    CREATE INDEX idx_edges_target_fqn ON edges(target_fqn);
    -- source_id stands in for source_fqn: edges always originate at a node row.
    CREATE INDEX idx_edges_source_rel ON edges(source_id, relation);
    CREATE INDEX idx_edges_target_rel ON edges(target_fqn, relation);
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

    CREATE TABLE node_embeddings (
        node_id      INTEGER PRIMARY KEY,
        model        TEXT NOT NULL,
        dim          INTEGER NOT NULL,
        vector       BLOB NOT NULL,
        content_hash TEXT NOT NULL DEFAULT '',
        FOREIGN KEY(node_id) REFERENCES nodes(id) ON DELETE CASCADE
    );
    CREATE INDEX idx_node_embeddings_model ON node_embeddings(model);
    `
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("migrate v%d: begin tx: %w", targetVersion, err)
	}
	if _, err := tx.Exec(drop); err != nil {
		tx.Rollback()
		return fmt.Errorf("migrate v%d: drop old: %w", targetVersion, err)
	}
	if _, err := tx.Exec(ddl); err != nil {
		tx.Rollback()
		return fmt.Errorf("migrate v%d: apply schema: %w", targetVersion, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migrate v%d: commit: %w", targetVersion, err)
	}
	if _, err := s.db.Exec(fmt.Sprintf("PRAGMA user_version = %d", targetVersion)); err != nil {
		return fmt.Errorf("migrate v%d: set user_version: %w", targetVersion, err)
	}
	return nil
}

// ensureFileStamps adds the mtime and size columns to a files table from
// before they existed. Additive, so a v5 database keeps its graph; rows from
// before carry a zero stamp and are hashed once more, then restamped.
func (s *Store) ensureFileStamps() error {
	for col, ddl := range map[string]string{
		"mtime_ns": "ALTER TABLE files ADD COLUMN mtime_ns INTEGER NOT NULL DEFAULT 0",
		"size":     "ALTER TABLE files ADD COLUMN size INTEGER NOT NULL DEFAULT -1",
	} {
		has, err := s.columnExists("files", col)
		if err != nil {
			return err
		}
		if has {
			continue
		}
		if _, err := s.db.Exec(ddl); err != nil {
			return fmt.Errorf("ckg store: add files.%s: %w", col, err)
		}
	}
	return nil
}

func (s *Store) columnExists(table, column string) (bool, error) {
	rows, err := s.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, fmt.Errorf("ckg store: table_info %s: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// ensureEmbeddingCache creates the content-hash vector cache if absent.
func (s *Store) ensureEmbeddingCache() error {
	_, err := s.db.Exec(`
        CREATE TABLE IF NOT EXISTS embedding_cache (
            model        TEXT NOT NULL,
            content_hash TEXT NOT NULL,
            dim          INTEGER NOT NULL,
            vector       BLOB NOT NULL,
            PRIMARY KEY (model, content_hash)
        )`)
	if err != nil {
		return fmt.Errorf("ckg store: embedding_cache: %w", err)
	}
	return nil
}

// ensureEmbeddingHashColumn adds content_hash to a node_embeddings table from
// before it existed; those rows keep their vectors and carry no hash.
func (s *Store) ensureEmbeddingHashColumn() error {
	has, err := s.columnExists("node_embeddings", "content_hash")
	if err != nil || has {
		return err
	}
	if _, err := s.db.Exec("ALTER TABLE node_embeddings ADD COLUMN content_hash TEXT NOT NULL DEFAULT ''"); err != nil {
		return fmt.Errorf("ckg store: add node_embeddings.content_hash: %w", err)
	}
	return nil
}

// ensureMemoryEmbeddings creates the memory vector table if it is absent.
// Idempotent and additive: it runs on every open, whatever the graph's schema
// version, so an old database gains the table without losing its vectors and a
// current one is untouched.
func (s *Store) ensureMemoryEmbeddings() error {
	// A table from a build before scopes existed cannot be pruned correctly,
	// and it is a pure cache, so it is dropped rather than migrated in place:
	// rebuilding costs one pass of embedding calls, keeping it costs silently
	// wrong eviction between sessions.
	var hasScope bool
	rows, err := s.db.Query(`PRAGMA table_info(memory_embeddings)`)
	if err == nil {
		existed := false
		for rows.Next() {
			var cid int
			var name, typ string
			var notNull, pk int
			var dflt any
			if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
				break
			}
			existed = true
			if name == "scope" {
				hasScope = true
			}
		}
		rows.Close()
		if existed && !hasScope {
			if _, err := s.db.Exec(`DROP TABLE memory_embeddings`); err != nil {
				return fmt.Errorf("migrate: drop pre-scope memory_embeddings: %w", err)
			}
		}
	}

	const ddl = `
    CREATE TABLE IF NOT EXISTS memory_embeddings (
        chunk_hash TEXT NOT NULL,
        model      TEXT NOT NULL,
        scope      TEXT NOT NULL DEFAULT '',
        dim        INTEGER NOT NULL,
        vector     BLOB NOT NULL,
        PRIMARY KEY (chunk_hash, model)
    );
    CREATE INDEX IF NOT EXISTS idx_memory_embeddings_model ON memory_embeddings(model);
    CREATE INDEX IF NOT EXISTS idx_memory_embeddings_scope ON memory_embeddings(model, scope);
    `
	if _, err := s.db.Exec(ddl); err != nil {
		return fmt.Errorf("migrate: memory_embeddings: %w", err)
	}
	return nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

// LockIndex / UnlockIndex wrap the graph write lock for callers that mutate
// the store outside Orchestrator.UpdateGraph (eval seeding). Do not call from
// SaveFileNodes — UpdateGraph already holds the lock.
func (s *Store) LockIndex() {
	if s == nil {
		return
	}
	s.indexMu.Lock()
}

func (s *Store) UnlockIndex() {
	if s == nil {
		return
	}
	s.indexMu.Unlock()
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

// FileStamps returns every indexed file's stamp, keyed by path. It takes
// the read lock: the scanner runs outside the write lock now, and Close
// must not pull the database from under it.
func (s *Store) FileStamps(ctx context.Context) (map[string]FileStamp, error) {
	s.indexMu.RLock()
	defer s.indexMu.RUnlock()
	return s.fileStampsUnlocked(ctx)
}

func (s *Store) fileStampsUnlocked(ctx context.Context) (map[string]FileStamp, error) {
	if s.db == nil {
		return nil, errStoreClosed
	}
	rows, err := s.db.QueryContext(ctx, "SELECT path, hash, mtime_ns, size FROM files")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]FileStamp)
	for rows.Next() {
		var path string
		var st FileStamp
		if err := rows.Scan(&path, &st.Hash, &st.MTimeNS, &st.Size); err != nil {
			return nil, err
		}
		m[path] = st
	}
	return m, rows.Err()
}

// errStoreClosed is what a refresh gets when Close won the race.
var errStoreClosed = fmt.Errorf("ckg store is closed")

// restampFiles records the stamps of files whose content the pass found
// unchanged but whose mtime or size moved (a touch, a checkout of the same
// bytes, a row from before stamps existed), so the next pass does not hash
// them again.
func (s *Store) restampFiles(ctx context.Context, stamps map[string]FileStamp) error {
	if len(stamps) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("restamp: begin tx: %w", err)
	}
	defer tx.Rollback()
	upd, err := tx.PrepareContext(ctx, `UPDATE files SET mtime_ns = ?, size = ? WHERE path = ? AND hash = ?`)
	if err != nil {
		return fmt.Errorf("restamp: prepare: %w", err)
	}
	defer upd.Close()
	for path, st := range stamps {
		if _, err := upd.ExecContext(ctx, st.MTimeNS, st.Size, path, st.Hash); err != nil {
			return fmt.Errorf("restamp %s: %w", path, err)
		}
	}
	return tx.Commit()
}

// SaveFileNodes upserts a single file's nodes/edges atomically, with no
// stamp: the next scan hashes the file once and restamps it. The
// orchestrator uses SaveFileNodesStamped.
//
// Edges may target symbols not yet indexed; their target_id will be NULL until
// the matching FQN is indexed (then a follow-up UPDATE in this same call
// resolves any previously-NULL edges whose target_fqn matches a freshly-inserted
// node — see step 5 below).
func (s *Store) SaveFileNodes(ctx context.Context, path, hash, lang, modulePath, pkgName string, nodes []Node, edges []Edge) error {
	return s.SaveFileNodesStamped(ctx, path, FileStamp{Hash: hash, Size: -1}, lang, modulePath, pkgName, nodes, edges)
}

// SaveFileNodesStamped is SaveFileNodes with the file's stamp, so the next
// scan can tell the file is unchanged from its mtime and size alone.
func (s *Store) SaveFileNodesStamped(ctx context.Context, path string, stamp FileStamp, lang, modulePath, pkgName string, nodes []Node, edges []Edge) error {
	if s.db == nil {
		return errStoreClosed
	}
	hash := stamp.Hash
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("save file nodes: begin tx: %w", err)
	}
	defer tx.Rollback()

	// 1. Cascading delete of the old file row clears its nodes and every edge
	//    whose source_id pointed at those nodes (ON DELETE CASCADE). Incoming
	//    edges keep the row with target_id NULL until RelinkUnresolvedEdges.
	if _, err := tx.ExecContext(ctx, "DELETE FROM files WHERE path = ?", path); err != nil {
		return fmt.Errorf("delete old file: %w", err)
	}

	// 2. Insert new files row.
	res, err := tx.ExecContext(ctx,
		`INSERT INTO files (path, hash, language, module_path, package, updated_at, mtime_ns, size)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		path, hash, lang, modulePath, pkgName, time.Now(), stamp.MTimeNS, stamp.Size)
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
			`INSERT INTO nodes (file_id, fqn, short_name, kind, line_start, line_end, complexity, package)
             VALUES (?, ?, ?, ?, ?, ?, ?, ?)
             ON CONFLICT(fqn) DO UPDATE SET
                 file_id    = excluded.file_id,
                 short_name = excluded.short_name,
                 kind       = excluded.kind,
                 line_start = excluded.line_start,
                 line_end   = excluded.line_end,
                 complexity = excluded.complexity,
                 package    = excluded.package
             RETURNING id`)
		if err != nil {
			return fmt.Errorf("prepare insert node: %w", err)
		}
		defer stmt.Close()

		for i := range nodes {
			pkg := nodes[i].Package
			if pkg == "" {
				pkg = pkgName
			}
			nodes[i].Package = pkg
			var newID int64
			err := stmt.QueryRowContext(ctx,
				fileID, nodes[i].FQN, nodes[i].ShortName, nodes[i].Kind,
				nodes[i].LineStart, nodes[i].LineEnd, nodes[i].Complexity, pkg,
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
			`INSERT OR IGNORE INTO edges (source_id, target_id, target_fqn, relation, is_external)
             VALUES (?, ?, ?, ?, ?)`)
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
			targetID, resolvedTargetFQN, err := resolveEdgeTarget(ctx, tx, sel, e.TargetFQN, e.Relation, pkgName)
			if err != nil {
				return err
			}
			targetFQN := e.TargetFQN
			if resolvedTargetFQN != "" {
				targetFQN = resolvedTargetFQN
			}
			ext := 0
			if e.IsExternal || (targetID == nil && isExternalTarget(targetFQN, modulePath)) {
				ext = 1
			}
			if _, err := ins.ExecContext(ctx, sourceID, targetID, targetFQN, e.Relation, ext); err != nil {
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
		//
		// OR IGNORE, like the edge INSERT above: rewriting target_fqn from "Run"
		// to the resolved FQN collides with an edge the same source already has
		// to that FQN (it called b.Run() qualified as well as Run unqualified).
		// A plain UPDATE failed the file's transaction on the UNIQUE index and
		// stopped UpdateGraph — it did on Orchestra's own internal/. The
		// short-name duplicate that stays behind is removed by delShortDup.
		updShort, err := tx.PrepareContext(ctx, `
			UPDATE OR IGNORE edges
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

		delShortDup, err := tx.PrepareContext(ctx, `
			DELETE FROM edges
			WHERE target_id IS NULL
			  AND relation IN ('calls', 'instantiates')
			  AND target_fqn = ?
			  AND EXISTS (
				  SELECT 1 FROM edges e2
				  WHERE e2.source_id = edges.source_id
				    AND e2.relation = edges.relation
				    AND e2.target_fqn = ?
			  )
			  AND NOT EXISTS (
				  SELECT 1 FROM nodes n2
				  WHERE n2.short_name = ? AND n2.id <> ?
			  )`)
		if err != nil {
			return fmt.Errorf("prepare short_name duplicate cleanup: %w", err)
		}
		defer delShortDup.Close()

		for _, n := range nodes {
			if n.ShortName == "" {
				continue
			}
			if _, err := updShort.ExecContext(ctx, n.ID, n.FQN, n.ShortName, n.ShortName, n.ID); err != nil {
				return fmt.Errorf("lazy resolve short_name %s: %w", n.ShortName, err)
			}
			if _, err := delShortDup.ExecContext(ctx, n.ShortName, n.FQN, n.ShortName, n.ID); err != nil {
				return fmt.Errorf("drop short_name duplicate %s: %w", n.ShortName, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	// The file's old nodes took their vectors with them (ON DELETE CASCADE).
	s.embedGen.Add(1)
	return nil
}

// DeleteFile deletes a file and cascades its deletion to nodes and edges.
func (s *Store) DeleteFile(ctx context.Context, path string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM files WHERE path = ?", path)
	if err == nil {
		s.embedGen.Add(1)
	}
	return err
}

var queryStopwords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "from": true,
	"this": true, "that": true, "are": true, "was": true, "will": true,
	"how": true, "what": true, "when": true, "where": true, "which": true,
	"add": true, "get": true, "set": true, "run": true, "new": true,
	"use": true, "can": true, "not": true, "has": true, "its": true,
}

// tokenizeQuery splits a user query into lowercase tokens (min 3 chars, no stopwords).
func tokenizeQuery(q string) []string {
	words := strings.FieldsFunc(q, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	seen := make(map[string]bool, len(words))
	out := make([]string, 0, len(words))
	for _, w := range words {
		w = strings.ToLower(w)
		if len(w) < 3 || queryStopwords[w] || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	return out
}

// FindRelevantNodes returns up to limit nodes whose FQN or short_name contains
// at least one token from query. Results are ranked by number of matching tokens.
func (s *Store) FindRelevantNodes(ctx context.Context, query string, limit int) ([]Node, error) {
	s.indexMu.RLock()
	defer s.indexMu.RUnlock()
	if s.db == nil {
		return nil, nil
	}
	tokens := tokenizeQuery(query)
	if len(tokens) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 12
	}

	// Build score expression: sum of per-token CASE matches.
	scoreParts := make([]string, len(tokens))
	whereParts := make([]string, len(tokens))
	// Args: CASE args (for SELECT) come first, then WHERE args, then LIMIT.
	args := make([]interface{}, 0, len(tokens)*4+1)

	for i, tok := range tokens {
		pattern := "%" + tok + "%"
		scoreParts[i] = "(CASE WHEN short_name LIKE ? OR fqn LIKE ? THEN 1 ELSE 0 END)"
		whereParts[i] = "(short_name LIKE ? OR fqn LIKE ?)"
		args = append(args, pattern, pattern) // CASE args
	}
	for _, tok := range tokens {
		pattern := "%" + tok + "%"
		args = append(args, pattern, pattern) // WHERE args
	}
	args = append(args, limit)

	q := fmt.Sprintf(`
		SELECT id, file_id, fqn, short_name, kind, line_start, line_end, complexity
		FROM (
			SELECT id, file_id, fqn, short_name, kind, line_start, line_end, complexity,
			       (%s) AS score
			FROM nodes
			WHERE %s
		)
		ORDER BY score DESC, fqn ASC
		LIMIT ?`,
		strings.Join(scoreParts, " + "),
		strings.Join(whereParts, " OR "),
	)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("find relevant nodes: %w", err)
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var n Node
		if err := rows.Scan(&n.ID, &n.FileID, &n.FQN, &n.ShortName, &n.Kind, &n.LineStart, &n.LineEnd, &n.Complexity); err != nil {
			return nil, fmt.Errorf("scan node: %w", err)
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
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

// FQNAtLine returns the FQN of the innermost (narrowest line range) non-package
// node that contains the given 1-based line number in filePath.
// Returns "" when no node covers the line.
func (s *Store) FQNAtLine(ctx context.Context, filePath string, lineno int) (string, error) {
	var fqn string
	err := s.db.QueryRowContext(ctx, `
		SELECT n.fqn
		FROM nodes n JOIN files f ON f.id = n.file_id
		WHERE f.path = ? AND n.line_start <= ? AND n.line_end >= ?
		  AND n.kind != 'package'
		ORDER BY (n.line_end - n.line_start) ASC
		LIMIT 1`, filePath, lineno, lineno).Scan(&fqn)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return fqn, err
}

// FileSymbol is a lightweight descriptor of a symbol used for file-level listing.
type FileSymbol struct {
	ShortName string
	Kind      string
	LineStart int
	LineEnd   int
}

// SymbolsInFile returns all non-package symbols in a file, ordered by line.
func (s *Store) SymbolsInFile(ctx context.Context, filePath string) ([]FileSymbol, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT n.short_name, n.kind, n.line_start, n.line_end
		FROM nodes n JOIN files f ON f.id = n.file_id
		WHERE f.path = ? AND n.kind != 'package'
		ORDER BY n.line_start`, filePath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FileSymbol
	for rows.Next() {
		var s FileSymbol
		if err := rows.Scan(&s.ShortName, &s.Kind, &s.LineStart, &s.LineEnd); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
