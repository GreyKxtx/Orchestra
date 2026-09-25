package ckg

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strings"
)

// EmbeddingItem ties a node id to its vector for SaveEmbeddings. ContentHash
// is the hash of the text the vector was made from; with it the vector is
// also kept in the content cache, and a re-index of the same text reuses it
// instead of calling the embedding endpoint again (DATA-5).
type EmbeddingItem struct {
	NodeID      int64
	Vector      []float32
	ContentHash string
}

// EmbeddedNode is a search hit returned by SearchSimilar.
type EmbeddedNode struct {
	Node  Node
	Path  string
	Score float32 // cosine similarity, [-1, 1]
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
        INSERT INTO node_embeddings (node_id, model, dim, vector, content_hash)
        VALUES (?, ?, ?, ?, ?)
        ON CONFLICT(node_id) DO UPDATE SET model = excluded.model, dim = excluded.dim, vector = excluded.vector, content_hash = excluded.content_hash
    `)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("save embeddings: prepare: %w", err)
	}
	defer stmt.Close()
	cache, err := tx.PrepareContext(ctx, `
        INSERT INTO embedding_cache (model, content_hash, dim, vector)
        VALUES (?, ?, ?, ?)
        ON CONFLICT(model, content_hash) DO UPDATE SET dim = excluded.dim, vector = excluded.vector
    `)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("save embeddings: prepare cache: %w", err)
	}
	defer cache.Close()
	for _, it := range items {
		if len(it.Vector) != dim || dim == 0 {
			continue
		}
		packed := PackVector(it.Vector)
		if _, err := stmt.ExecContext(ctx, it.NodeID, model, dim, packed, it.ContentHash); err != nil {
			tx.Rollback()
			return fmt.Errorf("save embeddings: insert node %d: %w", it.NodeID, err)
		}
		if it.ContentHash != "" {
			if _, err := cache.ExecContext(ctx, model, it.ContentHash, dim, packed); err != nil {
				tx.Rollback()
				return fmt.Errorf("save embeddings: cache node %d: %w", it.NodeID, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("save embeddings: commit: %w", err)
	}
	s.embedGen.Add(1)
	return nil
}

// CachedEmbeddings returns the vectors the content cache holds for model
// under the given content hashes: the text of a symbol that did not change
// across a re-index has the vector it had.
func (s *Store) CachedEmbeddings(ctx context.Context, model string, hashes []string) (map[string][]float32, error) {
	out := map[string][]float32{}
	if model == "" || len(hashes) == 0 {
		return out, nil
	}
	const chunk = 400 // SQLite's bound-parameter limit is 999 by default
	for i := 0; i < len(hashes); i += chunk {
		end := i + chunk
		if end > len(hashes) {
			end = len(hashes)
		}
		part := hashes[i:end]
		args := make([]any, 0, len(part)+1)
		args = append(args, model)
		marks := make([]string, len(part))
		for j, h := range part {
			args = append(args, h)
			marks[j] = "?"
		}
		rows, err := s.db.QueryContext(ctx, `SELECT content_hash, dim, vector FROM embedding_cache WHERE model = ? AND content_hash IN (`+strings.Join(marks, ",")+`)`, args...)
		if err != nil {
			return nil, fmt.Errorf("cached embeddings: %w", err)
		}
		for rows.Next() {
			var h string
			var dim int
			var blob []byte
			if err := rows.Scan(&h, &dim, &blob); err != nil {
				rows.Close()
				return nil, err
			}
			if v := UnpackVector(blob, dim); v != nil {
				out[h] = v
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ClearEmbeddings drops the model's vectors from the nodes (not from the
// content cache: a rebuild then costs only the symbols whose text changed).
func (s *Store) ClearEmbeddings(ctx context.Context, model string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM node_embeddings WHERE model = ?`, model); err != nil {
		return fmt.Errorf("clear embeddings: %w", err)
	}
	s.embedGen.Add(1)
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

// embedIndex is one model's vectors in memory, L2-normalized, so a query is
// one pass of dot products with a bounded heap instead of a decode of every
// BLOB from the database (DATA-5: 100K × 1536 floats is 600 MB per query).
type embedIndex struct {
	model string
	dim   int
	gen   uint64
	rows  []EmbeddedNode // Score unset
	vecs  []float32      // len(rows) × dim
}

// maxEmbedIndexBytes bounds the matrix; a model's vectors past it are
// searched from the database, as before.
const maxEmbedIndexBytes = 256 << 20

// embedIndexFor returns the matrix for model at the query's dimension,
// rebuilding it when a vector or a node changed since it was built. nil
// means the vectors do not fit in memory.
func (s *Store) embedIndexFor(ctx context.Context, model string, dim int) (*embedIndex, error) {
	s.embedMu.Lock()
	defer s.embedMu.Unlock()
	gen := s.embedGen.Load()
	if idx := s.embedIdx; idx != nil && idx.model == model && idx.dim == dim && idx.gen == gen {
		return idx, nil
	}
	s.embedLoads.Add(1)
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM node_embeddings WHERE model = ? AND dim = ?`, model, dim).Scan(&n); err != nil {
		return nil, fmt.Errorf("embed index: count: %w", err)
	}
	if n*dim*4 > maxEmbedIndexBytes {
		s.embedIdx = nil
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
        SELECT n.id, n.file_id, n.fqn, n.short_name, n.kind, n.line_start, n.line_end, n.complexity,
               f.path, e.vector
        FROM node_embeddings e
        JOIN nodes n ON n.id = e.node_id
        JOIN files f ON f.id = n.file_id
        WHERE e.model = ? AND e.dim = ?
    `, model, dim)
	if err != nil {
		return nil, fmt.Errorf("embed index: load: %w", err)
	}
	defer rows.Close()
	idx := &embedIndex{model: model, dim: dim, gen: gen, rows: make([]EmbeddedNode, 0, n), vecs: make([]float32, 0, n*dim)}
	for rows.Next() {
		var r EmbeddedNode
		var blob []byte
		if err := rows.Scan(&r.Node.ID, &r.Node.FileID, &r.Node.FQN, &r.Node.ShortName, &r.Node.Kind,
			&r.Node.LineStart, &r.Node.LineEnd, &r.Node.Complexity, &r.Path, &blob); err != nil {
			return nil, err
		}
		vec := UnpackVector(blob, dim)
		if vec == nil || !normalize(vec) {
			continue
		}
		idx.rows = append(idx.rows, r)
		idx.vecs = append(idx.vecs, vec...)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.embedIdx = idx
	return idx, nil
}

// normalize scales v to unit length in place; false when it is zero.
func normalize(v []float32) bool {
	var mag float64
	for _, x := range v {
		mag += float64(x) * float64(x)
	}
	if mag == 0 {
		return false
	}
	inv := float32(1 / math.Sqrt(mag))
	for i := range v {
		v[i] *= inv
	}
	return true
}

// bestK keeps the K best scores seen: a min-heap of size K.
type bestK struct {
	k     int
	items []scored
}

type scored struct {
	i     int
	score float32
}

func (h *bestK) push(i int, score float32) {
	if len(h.items) < h.k {
		h.items = append(h.items, scored{i, score})
		h.up(len(h.items) - 1)
		return
	}
	if score <= h.items[0].score {
		return
	}
	h.items[0] = scored{i, score}
	h.down(0)
}

func (h *bestK) up(j int) {
	for j > 0 {
		p := (j - 1) / 2
		if h.items[p].score <= h.items[j].score {
			return
		}
		h.items[p], h.items[j] = h.items[j], h.items[p]
		j = p
	}
}

func (h *bestK) down(j int) {
	n := len(h.items)
	for {
		l, r, m := 2*j+1, 2*j+2, j
		if l < n && h.items[l].score < h.items[m].score {
			m = l
		}
		if r < n && h.items[r].score < h.items[m].score {
			m = r
		}
		if m == j {
			return
		}
		h.items[m], h.items[j] = h.items[j], h.items[m]
		j = m
	}
}

// SearchSimilar returns the top-K nodes by cosine similarity to query,
// restricted to embeddings stored under the given model: a pass over the
// in-memory matrix, or over the database when the matrix would not fit.
func (s *Store) SearchSimilar(ctx context.Context, model string, query []float32, topK int) ([]EmbeddedNode, error) {
	if topK <= 0 {
		topK = 10
	}
	if len(query) == 0 {
		return nil, fmt.Errorf("search similar: query vector is empty")
	}
	idx, err := s.embedIndexFor(ctx, model, len(query))
	if err != nil {
		return nil, err
	}
	if idx == nil {
		return s.searchSimilarStreaming(ctx, model, query, topK)
	}
	q := append([]float32(nil), query...)
	if !normalize(q) {
		return nil, nil
	}
	dim := idx.dim
	best := &bestK{k: topK}
	for i := range idx.rows {
		row := idx.vecs[i*dim : (i+1)*dim]
		var dot float64
		for j := range q {
			dot += float64(q[j]) * float64(row[j])
		}
		best.push(i, float32(dot))
	}
	hits := make([]EmbeddedNode, 0, len(best.items))
	for _, it := range best.items {
		h := idx.rows[it.i]
		h.Score = it.score
		hits = append(hits, h)
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	return hits, nil
}

// searchSimilarStreaming decodes every vector of the model from the
// database: the path for a model whose vectors exceed maxEmbedIndexBytes.
func (s *Store) searchSimilarStreaming(ctx context.Context, model string, query []float32, topK int) ([]EmbeddedNode, error) {
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
