package ckg

import (
	"context"
	"sort"
	"strings"
)

// OutlineSymbol is one indexed symbol of a file — what the Graph view lists
// when a file is selected, in the order the symbols appear in the source.
type OutlineSymbol struct {
	Name      string `json:"name"`
	FQN       string `json:"fqn,omitempty"`
	Kind      string `json:"kind"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
	CallsOut  int    `json:"calls_out"`
	CallsIn   int    `json:"calls_in"`
}

// FileOutline is a file's indexed symbols. Available is false when the file
// is not in the graph store at all (never scanned, excluded, or deleted).
type FileOutline struct {
	Path      string          `json:"path"`
	Language  string          `json:"language"`
	Available bool            `json:"available"`
	Symbols   []OutlineSymbol `json:"symbols"`
}

// BuildFileOutline reads one file's symbols out of the graph store, with how
// many relations each one takes part in. Cheap by construction: three small
// queries keyed on the file, never the whole graph.
func BuildFileOutline(ctx context.Context, store *Store, filePath string) (*FileOutline, error) {
	out := &FileOutline{Path: filePath, Symbols: []OutlineSymbol{}}
	if store == nil || store.db == nil {
		return out, nil
	}
	clean := strings.TrimSpace(strings.ReplaceAll(filePath, "\\", "/"))
	out.Path = clean
	if clean == "" {
		return out, nil
	}

	var fileID int64
	var lang string
	row := store.db.QueryRowContext(ctx, "SELECT id, language FROM files WHERE path = ?", clean)
	if err := row.Scan(&fileID, &lang); err != nil {
		// No such file in the store is an answer, not an error.
		return out, nil
	}
	out.Available = true
	out.Language = lang

	rows, err := store.db.QueryContext(ctx,
		"SELECT id, fqn, short_name, kind, line_start, line_end FROM nodes WHERE file_id = ?", fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	byID := map[int64]int{}
	for rows.Next() {
		var id int64
		var fqn, name, kind string
		var start, end int
		if err := rows.Scan(&id, &fqn, &name, &kind, &start, &end); err != nil {
			continue
		}
		byID[id] = len(out.Symbols)
		ids = append(ids, id)
		out.Symbols = append(out.Symbols, OutlineSymbol{
			Name:      name,
			FQN:       fqn,
			Kind:      kind,
			LineStart: start,
			LineEnd:   end,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return out, nil
	}

	countEdges := func(column string, set func(i, n int)) error {
		args := make([]any, len(ids))
		for i, id := range ids {
			args[i] = id
		}
		q := "SELECT " + column + ", COUNT(*) FROM edges WHERE " + column + " IN (" +
			strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ") GROUP BY " + column
		r, err := store.db.QueryContext(ctx, q, args...)
		if err != nil {
			return err
		}
		defer r.Close()
		for r.Next() {
			var id int64
			var n int
			if err := r.Scan(&id, &n); err != nil {
				continue
			}
			if i, ok := byID[id]; ok {
				set(i, n)
			}
		}
		return r.Err()
	}
	if err := countEdges("source_id", func(i, n int) { out.Symbols[i].CallsOut = n }); err != nil {
		return nil, err
	}
	if err := countEdges("target_id", func(i, n int) { out.Symbols[i].CallsIn = n }); err != nil {
		return nil, err
	}

	sort.SliceStable(out.Symbols, func(i, j int) bool {
		if out.Symbols[i].LineStart != out.Symbols[j].LineStart {
			return out.Symbols[i].LineStart < out.Symbols[j].LineStart
		}
		return out.Symbols[i].Name < out.Symbols[j].Name
	})
	return out, nil
}
