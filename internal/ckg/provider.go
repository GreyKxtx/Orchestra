package ckg

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Provider serves as the API for LLM to query the Code Knowledge Graph.
type Provider struct {
	store *Store
	root  string
}

// NewProvider creates a new CKG provider.
func NewProvider(store *Store, root string) *Provider {
	return &Provider{store: store, root: root}
}

// ExploreSymbol retrieves the exact source code definition of a symbol
// and its dependencies (edges) from the database without reading the whole file.
func (p *Provider) ExploreSymbol(ctx context.Context, query string) (string, error) {
	var rows *sql.Rows
	var err error

	if IsLikelyFQN(query) {
		rows, err = p.store.db.QueryContext(ctx, `
            SELECT n.id, n.fqn, n.short_name, n.kind, n.line_start, n.line_end, f.path
            FROM nodes n JOIN files f ON n.file_id = f.id
            WHERE n.fqn = ?`, query)
	} else {
		rows, err = p.store.db.QueryContext(ctx, `
            SELECT n.id, n.fqn, n.short_name, n.kind, n.line_start, n.line_end, f.path
            FROM nodes n JOIN files f ON n.file_id = f.id
            WHERE n.short_name = ?`, query)
	}
	if err != nil {
		return "", err
	}
	defer rows.Close()

	type hit struct {
		id        int64
		fqn       string
		shortName string
		kind      string
		lineStart int
		lineEnd   int
		relPath   string
	}
	var hits []hit
	for rows.Next() {
		var h hit
		if err := rows.Scan(&h.id, &h.fqn, &h.shortName, &h.kind, &h.lineStart, &h.lineEnd, &h.relPath); err != nil {
			continue
		}
		hits = append(hits, h)
	}

	if len(hits) == 0 {
		return p.fuzzyFallback(ctx, query)
	}
	if len(hits) > 1 && !IsLikelyFQN(query) {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Запрос '%s' неоднозначен — найдено %d символов с одинаковым short-name. Уточните FQN:\n\n", query, len(hits)))
		for _, h := range hits {
			sb.WriteString(fmt.Sprintf("- `%s` (%s в %s, строки %d-%d)\n", h.fqn, h.kind, h.relPath, h.lineStart, h.lineEnd))
		}
		return sb.String(), nil
	}

	var sb strings.Builder
	for _, h := range hits {
		absPath := filepath.Join(p.root, filepath.FromSlash(h.relPath))
		content, err := os.ReadFile(absPath)
		if err != nil {
			sb.WriteString(fmt.Sprintf("Error reading file %s: %v\n\n", h.relPath, err))
			continue
		}
		lines := strings.Split(string(content), "\n")
		ls, le := h.lineStart, h.lineEnd
		if ls < 1 {
			ls = 1
		}
		if le > len(lines) {
			le = len(lines)
		}
		snippet := strings.Join(lines[ls-1:le], "\n")

		sb.WriteString(fmt.Sprintf("### `%s` (%s) в `%s` (строки %d-%d)\n", h.fqn, h.kind, h.relPath, ls, le))
		ext := filepath.Ext(h.relPath)
		sb.WriteString(fmt.Sprintf("```%s\n%s\n```\n\n", LanguageFromExt(ext), snippet))

		// Callers
		cRows, _ := p.store.db.QueryContext(ctx,
			`SELECT n.fqn, e.relation FROM edges e JOIN nodes n ON e.source_id = n.id
             WHERE e.target_fqn = ?`, h.fqn)
		if cRows != nil {
			first := true
			for cRows.Next() {
				if first {
					sb.WriteString("**Вызывается из (callers):**\n")
					first = false
				}
				var srcFQN, rel string
				if cRows.Scan(&srcFQN, &rel) == nil {
					sb.WriteString(fmt.Sprintf("- `%s` (%s)\n", srcFQN, rel))
				}
			}
			cRows.Close()
			if !first {
				sb.WriteString("\n")
			}
		}

		// Callees
		dRows, _ := p.store.db.QueryContext(ctx,
			`SELECT e.target_fqn, e.relation FROM edges e WHERE e.source_id = ?`, h.id)
		if dRows != nil {
			first := true
			for dRows.Next() {
				if first {
					sb.WriteString("**Зависит от (callees):**\n")
					first = false
				}
				var tgtFQN, rel string
				if dRows.Scan(&tgtFQN, &rel) == nil {
					sb.WriteString(fmt.Sprintf("- `%s` (%s)\n", tgtFQN, rel))
				}
			}
			dRows.Close()
			if !first {
				sb.WriteString("\n")
			}
		}
	}
	return sb.String(), nil
}

func (p *Provider) fuzzyFallback(ctx context.Context, query string) (string, error) {
	rows, err := p.store.db.QueryContext(ctx, `
        SELECT n.fqn, n.kind, f.path FROM nodes n JOIN files f ON n.file_id = f.id
        WHERE n.short_name LIKE ? LIMIT 5`, "%"+query+"%")
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var sugg []string
	for rows.Next() {
		var fqn, kind, path string
		if rows.Scan(&fqn, &kind, &path) == nil {
			sugg = append(sugg, fmt.Sprintf("- `%s` (%s в %s)", fqn, kind, path))
		}
	}
	if len(sugg) == 0 {
		return fmt.Sprintf("Символ '%s' не найден в графе.", query), nil
	}
	return fmt.Sprintf("Символ '%s' не найден точно. Похожие:\n%s", query, strings.Join(sugg, "\n")), nil
}

// Callers returns all nodes that have a "calls" or "instantiates" edge whose
// target_fqn equals the given fqn.
func (p *Provider) Callers(ctx context.Context, fqn string) ([]Node, error) {
	rows, err := p.store.db.QueryContext(ctx, `
        SELECT n.id, n.file_id, n.fqn, n.short_name, n.kind, n.line_start, n.line_end, n.complexity
        FROM edges e JOIN nodes n ON e.source_id = n.id
        WHERE e.target_fqn = ? AND e.relation IN ('calls','instantiates')`, fqn)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

// Callees returns all edges originating from the node with the given fqn.
func (p *Provider) Callees(ctx context.Context, fqn string) ([]Edge, error) {
	rows, err := p.store.db.QueryContext(ctx, `
        SELECT e.target_fqn, e.relation FROM edges e
        JOIN nodes n ON e.source_id = n.id
        WHERE n.fqn = ?`, fqn)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Edge
	for rows.Next() {
		e := Edge{SourceFQN: fqn}
		if err := rows.Scan(&e.TargetFQN, &e.Relation); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// Importers returns FQNs of all packages that import the given package FQN.
func (p *Provider) Importers(ctx context.Context, packageFQN string) ([]string, error) {
	rows, err := p.store.db.QueryContext(ctx, `
        SELECT DISTINCT n.fqn FROM edges e
        JOIN nodes n ON e.source_id = n.id
        WHERE e.target_fqn = ? AND e.relation = 'imports' AND n.kind = 'package'`, packageFQN)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func scanNodes(rows *sql.Rows) ([]Node, error) {
	var out []Node
	for rows.Next() {
		var n Node
		if err := rows.Scan(&n.ID, &n.FileID, &n.FQN, &n.ShortName, &n.Kind, &n.LineStart, &n.LineEnd, &n.Complexity); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}
