package ckg

import (
	"context"
	"fmt"
	"strings"
)

// Memory embeddings live in the CKG database but not in the code graph.
//
// The obvious move was to reuse node_embeddings with a "memory" node kind, and
// it does not survive contact: node_embeddings.node_id is a foreign key into
// nodes, nodes needs a file_id and a line range, and every graph consumer
// (explore, symbols, repo_map) reads nodes — memory would surface as code
// symbols in all of them unless each one filtered it out, and one forgotten
// filter is a silent wrong answer. A separate table keyed by the chunk's
// content hash owes the graph nothing: no synthetic file rows, no filters, and
// no vectors lost when a file is deleted and its nodes cascade away.

// MemoryVector is one chunk's vector plus the scope that owns it.
type MemoryVector struct {
	Hash string
	// Scope names who the chunk belongs to: the workspace's durable layers
	// share one scope, and each session has its own. Prune works per scope so
	// one session's search cannot evict another session's vectors.
	Scope  string
	Vector []float32
}

// SaveMemoryEmbeddings upserts vectors for the given model, keyed by chunk
// hash. Vectors must share a dim; mismatched ones are skipped rather than
// failing the batch, so a noisy embedding server cannot lose the good rows.
func (s *Store) SaveMemoryEmbeddings(ctx context.Context, model string, vecs []MemoryVector) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("save memory embeddings: no store")
	}
	if strings.TrimSpace(model) == "" {
		return fmt.Errorf("save memory embeddings: model is empty")
	}
	if len(vecs) == 0 {
		return nil
	}
	dim := len(vecs[0].Vector)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("save memory embeddings: begin tx: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, `
        INSERT INTO memory_embeddings (chunk_hash, model, scope, dim, vector)
        VALUES (?, ?, ?, ?, ?)
        ON CONFLICT(chunk_hash, model) DO UPDATE SET
            scope = excluded.scope, dim = excluded.dim, vector = excluded.vector
    `)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("save memory embeddings: prepare: %w", err)
	}
	defer stmt.Close()
	for _, v := range vecs {
		if len(v.Vector) != dim || dim == 0 {
			continue
		}
		if _, err := stmt.ExecContext(ctx, v.Hash, model, v.Scope, dim, PackVector(v.Vector)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("save memory embeddings: insert %s: %w", v.Hash, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("save memory embeddings: commit: %w", err)
	}
	return nil
}

// LoadMemoryEmbeddings returns the stored vectors for the given hashes under
// model. Hashes with no vector are simply absent from the result — that is the
// caller's list of what still needs embedding.
func (s *Store) LoadMemoryEmbeddings(ctx context.Context, model string, hashes []string) (map[string][]float32, error) {
	out := map[string][]float32{}
	if s == nil || s.db == nil || strings.TrimSpace(model) == "" || len(hashes) == 0 {
		return out, nil
	}
	// SQLite caps variables per statement (999 by default), and memory can hold
	// more chunks than that, so the lookup is chunked rather than one big IN.
	const batch = 400
	for start := 0; start < len(hashes); start += batch {
		end := start + batch
		if end > len(hashes) {
			end = len(hashes)
		}
		part := hashes[start:end]
		args := make([]any, 0, len(part)+1)
		args = append(args, model)
		placeholders := make([]string, len(part))
		for i, h := range part {
			placeholders[i] = "?"
			args = append(args, h)
		}
		q := `SELECT chunk_hash, dim, vector FROM memory_embeddings WHERE model = ? AND chunk_hash IN (` +
			strings.Join(placeholders, ",") + `)`
		rows, err := s.db.QueryContext(ctx, q, args...)
		if err != nil {
			return nil, fmt.Errorf("load memory embeddings: %w", err)
		}
		for rows.Next() {
			var hash string
			var dim int
			var blob []byte
			if err := rows.Scan(&hash, &dim, &blob); err != nil {
				rows.Close()
				return nil, fmt.Errorf("load memory embeddings: scan: %w", err)
			}
			if v := UnpackVector(blob, dim); v != nil {
				out[hash] = v
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, fmt.Errorf("load memory embeddings: %w", err)
		}
	}
	return out, nil
}

// PruneMemoryEmbeddings deletes this model's vectors that belong to one of
// scopes and whose hash is not in keep.
//
// Scoping is the whole point. A search sees the workspace's durable layers
// plus its own session's — never another session's — so "delete everything
// this search did not see" would evict the other sessions' vectors, and with
// two sessions open each would keep re-embedding what the other just threw
// away. Pruning only the scopes the caller actually enumerated makes the
// sweep exact.
//
// An empty keep set still clears those scopes: memory really can be emptied,
// and reading that as "keep everything" would strand vectors for text that no
// longer exists. An empty scope list prunes nothing.
func (s *Store) PruneMemoryEmbeddings(ctx context.Context, model string, scopes []string, keep []string) error {
	if s == nil || s.db == nil || strings.TrimSpace(model) == "" || len(scopes) == 0 {
		return nil
	}
	scopeArgs := make([]any, 0, len(scopes)+1)
	scopeArgs = append(scopeArgs, model)
	marks := make([]string, len(scopes))
	for i, sc := range scopes {
		marks[i] = "?"
		scopeArgs = append(scopeArgs, sc)
	}
	scopeFilter := ` AND scope IN (` + strings.Join(marks, ",") + `)`

	if len(keep) == 0 {
		if _, err := s.db.ExecContext(ctx,
			`DELETE FROM memory_embeddings WHERE model = ?`+scopeFilter, scopeArgs...); err != nil {
			return fmt.Errorf("prune memory embeddings: %w", err)
		}
		return nil
	}
	// Build the survivor set in a temp table: a NOT IN with thousands of
	// variables would exceed SQLite's per-statement limit.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("prune memory embeddings: begin tx: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE IF NOT EXISTS memory_keep (chunk_hash TEXT PRIMARY KEY)`); err != nil {
		return fmt.Errorf("prune memory embeddings: temp table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM memory_keep`); err != nil {
		return fmt.Errorf("prune memory embeddings: clear temp: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO memory_keep (chunk_hash) VALUES (?)`)
	if err != nil {
		return fmt.Errorf("prune memory embeddings: prepare: %w", err)
	}
	for _, h := range keep {
		if _, err := stmt.ExecContext(ctx, h); err != nil {
			stmt.Close()
			return fmt.Errorf("prune memory embeddings: fill temp: %w", err)
		}
	}
	stmt.Close()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM memory_embeddings
         WHERE model = ?`+scopeFilter+`
           AND chunk_hash NOT IN (SELECT chunk_hash FROM memory_keep)`, scopeArgs...); err != nil {
		return fmt.Errorf("prune memory embeddings: delete: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("prune memory embeddings: commit: %w", err)
	}
	return nil
}

// CountMemoryEmbeddings reports how many vectors are stored for the model.
func (s *Store) CountMemoryEmbeddings(ctx context.Context, model string) (int, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_embeddings WHERE model = ?`, model).Scan(&n)
	return n, err
}
