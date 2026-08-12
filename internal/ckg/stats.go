package ckg

import (
	"context"
	"strings"
)

// IndexStats holds aggregate CKG store counters for UI / RPC status.
type IndexStats struct {
	Files      int `json:"files"`
	Nodes      int `json:"nodes"`
	Edges      int `json:"edges"`
	Embeddings int `json:"embeddings"`
	MissingEmb int `json:"missing_embeddings"`
	// Node-kind breakdown for UI cards.
	Funcs    int `json:"funcs"`    // kind IN (func, method)
	Types    int `json:"types"`    // kind IN (struct, interface, type)
	Packages int `json:"packages"` // kind = package
	Tests    int `json:"tests"`    // func/method nodes in test files or Test*-named
	// Langs maps language → file count (e.g. {"go": 120, "typescript": 30}).
	Langs map[string]int `json:"langs,omitempty"`
}

// IndexStats returns file/node/edge counts and embedding coverage for model.
// When model is empty, embedding counts are omitted (0).
func (s *Store) IndexStats(ctx context.Context, model string) (IndexStats, error) {
	var st IndexStats
	if s == nil || s.db == nil {
		return st, nil
	}
	// A graph refresh writes files one-by-one. Wait for it to finish so status
	// never exposes a misleading partial snapshot (for example 5 JS files
	// before the scanner reaches TS/TSX sources).
	s.indexMu.Lock()
	defer s.indexMu.Unlock()

	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM files`).Scan(&st.Files); err != nil {
		return st, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM nodes`).Scan(&st.Nodes); err != nil {
		return st, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM edges`).Scan(&st.Edges); err != nil {
		return st, err
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM nodes WHERE kind IN ('func','method')`).Scan(&st.Funcs); err != nil {
		return st, err
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM nodes WHERE kind IN ('struct','interface','type')`).Scan(&st.Types); err != nil {
		return st, err
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM nodes WHERE kind = 'package'`).Scan(&st.Packages); err != nil {
		return st, err
	}
	// Tests: functions/methods living in test files (Go/TS/JS/Python naming) or Go Test* funcs in _test.go.
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM nodes n
		JOIN files f ON f.id = n.file_id
		WHERE n.kind IN ('func','method') AND (
			f.path LIKE '%_test.go'
			OR f.path LIKE '%.test.ts' OR f.path LIKE '%.test.tsx'
			OR f.path LIKE '%.test.js' OR f.path LIKE '%.spec.ts'
			OR f.path LIKE '%.spec.js' OR f.path LIKE '%.spec.tsx'
			OR f.path LIKE 'test_%.py' OR f.path LIKE '%/test_%.py'
		)`).Scan(&st.Tests); err != nil {
		return st, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT language, COUNT(*) FROM files WHERE language != '' GROUP BY language`)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	for rows.Next() {
		var lang string
		var n int
		if err := rows.Scan(&lang, &n); err != nil {
			return st, err
		}
		if st.Langs == nil {
			st.Langs = map[string]int{}
		}
		st.Langs[lang] = n
	}
	if err := rows.Err(); err != nil {
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
