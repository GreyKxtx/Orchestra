package ckg

import (
	"context"
	"strings"
)

// IndexStats holds aggregate CKG store counters for UI / RPC status.
type IndexStats struct {
	Files       int `json:"files"`
	Nodes       int `json:"nodes"`
	Edges       int `json:"edges"`
	Embeddings  int `json:"embeddings"`
	MissingEmb  int `json:"missing_embeddings"`
}

// IndexStats returns file/node/edge counts and embedding coverage for model.
// When model is empty, embedding counts are omitted (0).
func (s *Store) IndexStats(ctx context.Context, model string) (IndexStats, error) {
	var st IndexStats
	if s == nil || s.db == nil {
		return st, nil
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM files`).Scan(&st.Files); err != nil {
		return st, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM nodes`).Scan(&st.Nodes); err != nil {
		return st, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM edges`).Scan(&st.Edges); err != nil {
		return st, err
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return st, nil
	}
	n, err := s.CountEmbeddings(ctx, model)
	if err != nil {
		return st, err
	}
	st.Embeddings = n
	missing, err := s.MissingEmbeddings(ctx, model, 0)
	if err != nil {
		return st, err
	}
	st.MissingEmb = len(missing)
	return st, nil
}
