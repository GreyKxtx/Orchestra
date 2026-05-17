package ckg

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
)

// EmbeddingItem ties a node id to its vector for SaveEmbeddings.
type EmbeddingItem struct {
	NodeID int64
	Vector []float32
}

// EmbeddedNode is a search hit returned by SearchSimilar.
type EmbeddedNode struct {
	Node   Node
	Path   string
	Score  float32 // cosine similarity, [-1, 1]
}

// MissingEmbedding describes a node that has no embedding for the
// configured model yet. Path and source range are returned so callers
// can read the snippet without an extra query.
type MissingEmbedding struct {
	NodeID    int64
	FQN       string
	ShortName string
	Kind      string
	Path      string
	LineStart int
	LineEnd   int
}

// PackVector encodes a float32 slice as little-endian bytes for BLOB storage.
func PackVector(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

// UnpackVector decodes a BLOB into a float32 slice with the given dim.
// Returns nil when the byte length doesn't match dim*4.
func UnpackVector(b []byte, dim int) []float32 {
	if len(b) != dim*4 {
		return nil
	}
	v := make([]float32, dim)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

// SaveEmbeddings upserts a batch of (nodeID, vector) pairs for the given
// model. Vectors must have a consistent dim within the batch — the first
// vector's length is taken as authoritative; mismatched rows are skipped
// (with no error) to keep partial progress on a noisy embedding server.
func (s *Store) SaveEmbeddings(ctx context.Context, model string, items []EmbeddingItem) error {
	if model == "" {
		return fmt.Errorf("save embeddings: model is empty")
	}
	if len(items) == 0 {
		return nil
	}
	dim := len(items[0].Vector)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("save embeddings: begin tx: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, `
        INSERT INTO node_embeddings (node_id, model, dim, vector)
        VALUES (?, ?, ?, ?)
        ON CONFLICT(node_id) DO UPDATE SET model = excluded.model, dim = excluded.dim, vector = excluded.vector
    `)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("save embeddings: prepare: %w", err)
	}
	defer stmt.Close()
	for _, it := range items {
		if len(it.Vector) != dim || dim == 0 {
			continue
		}
		if _, err := stmt.ExecContext(ctx, it.NodeID, model, dim, PackVector(it.Vector)); err != nil {
			tx.Rollback()
			return fmt.Errorf("save embeddings: insert node %d: %w", it.NodeID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("save embeddings: commit: %w", err)
	}
	return nil
}

// CountEmbeddings returns the number of embeddings stored for the model.
func (s *Store) CountEmbeddings(ctx context.Context, model string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM node_embeddings WHERE model = ?`, model).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// MissingEmbeddings returns indexable nodes that lack an embedding for
// model. Limited to func/method/struct/interface/type — package-level
// nodes are too coarse to embed usefully. limit ≤ 0 means no cap.
func (s *Store) MissingEmbeddings(ctx context.Context, model string, limit int) ([]MissingEmbedding, error) {
	q := `
        SELECT n.id, n.fqn, n.short_name, n.kind, f.path, n.line_start, n.line_end
        FROM nodes n
        JOIN files f ON f.id = n.file_id
        LEFT JOIN node_embeddings e ON e.node_id = n.id AND e.model = ?
        WHERE e.node_id IS NULL
          AND n.kind IN ('func','method','struct','interface','type')
        ORDER BY n.id
    `
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := s.db.QueryContext(ctx, q, model)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MissingEmbedding
	for rows.Next() {
		var m MissingEmbedding
		if err := rows.Scan(&m.NodeID, &m.FQN, &m.ShortName, &m.Kind, &m.Path, &m.LineStart, &m.LineEnd); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SearchSimilar returns the top-K nodes by cosine similarity to query,
// restricted to embeddings stored under the given model. Brute-force
// scan; fine up to ~100k vectors.
func (s *Store) SearchSimilar(ctx context.Context, model string, query []float32, topK int) ([]EmbeddedNode, error) {
	if topK <= 0 {
		topK = 10
	}
	if len(query) == 0 {
		return nil, fmt.Errorf("search similar: query vector is empty")
	}
	rows, err := s.db.QueryContext(ctx, `
        SELECT n.id, n.file_id, n.fqn, n.short_name, n.kind, n.line_start, n.line_end, n.complexity,
               f.path, e.dim, e.vector
        FROM node_embeddings e
        JOIN nodes n ON n.id = e.node_id
        JOIN files f ON f.id = n.file_id
        WHERE e.model = ?
    `, model)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Precompute query magnitude.
	var qMag float64
	for _, v := range query {
		qMag += float64(v) * float64(v)
	}
	qMag = math.Sqrt(qMag)
	if qMag == 0 {
		return nil, nil
	}

	var hits []EmbeddedNode
	for rows.Next() {
		var n Node
		var path string
		var dim int
		var blob []byte
		if err := rows.Scan(&n.ID, &n.FileID, &n.FQN, &n.ShortName, &n.Kind, &n.LineStart, &n.LineEnd, &n.Complexity, &path, &dim, &blob); err != nil {
			return nil, err
		}
		if dim != len(query) {
			continue // dim mismatch — likely older model; skip silently
		}
		vec := UnpackVector(blob, dim)
		if vec == nil {
			continue
		}
		score := cosine32(query, vec, qMag)
		hits = append(hits, EmbeddedNode{Node: n, Path: path, Score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > topK {
		hits = hits[:topK]
	}
	return hits, nil
}

// cosine32 is a fast cosine: caller supplies pre-computed |query|.
func cosine32(query, doc []float32, queryMag float64) float32 {
	var dot, docMag float64
	for i := range query {
		dot += float64(query[i]) * float64(doc[i])
		docMag += float64(doc[i]) * float64(doc[i])
	}
	docMag = math.Sqrt(docMag)
	if docMag == 0 {
		return 0
	}
	return float32(dot / (queryMag * docMag))
}
