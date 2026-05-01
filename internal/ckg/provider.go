package ckg

import (
	"context"
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
func (p *Provider) ExploreSymbol(ctx context.Context, name string) (string, error) {
	query := `
		SELECT n.id, n.type, n.line_start, n.line_end, f.path
		FROM nodes n
		JOIN files f ON n.file_id = f.id
		WHERE n.name = ?
	`
	rows, err := p.store.db.QueryContext(ctx, query, name)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var sb strings.Builder
	found := false

	for rows.Next() {
		found = true
		var id int64
		var nodeType, relPath string
		var lineStart, lineEnd int
		if err := rows.Scan(&id, &nodeType, &lineStart, &lineEnd, &relPath); err != nil {
			continue
		}

		// Read the exact snippet from the file
		absPath := filepath.Join(p.root, filepath.FromSlash(relPath))
		content, err := os.ReadFile(absPath)
		if err != nil {
			sb.WriteString(fmt.Sprintf("Error reading file %s: %v\n\n", relPath, err))
			continue
		}

		lines := strings.Split(string(content), "\n")
		// Bounds checking (1-based index to 0-based slice)
		if lineStart < 1 {
			lineStart = 1
		}
		if lineEnd > len(lines) {
			lineEnd = len(lines)
		}

		snippet := strings.Join(lines[lineStart-1:lineEnd], "\n")

		sb.WriteString(fmt.Sprintf("### Definition: `%s` (%s) in `%s` (lines %d-%d)\n", name, nodeType, relPath, lineStart, lineEnd))
		
		// Map extension for markdown code block formatting
		ext := filepath.Ext(relPath)
		lang := LanguageFromExt(ext)
		sb.WriteString(fmt.Sprintf("```%s\n", lang))
		sb.WriteString(snippet)
		sb.WriteString("\n```\n\n")

		// Retrieve edges (who calls or uses this symbol)
		edgesQuery := `
			SELECT source_name, relation
			FROM edges
			WHERE target_name = ?
		`
		eRows, err := p.store.db.QueryContext(ctx, edgesQuery, name)
		if err == nil {
			hasEdges := false
			for eRows.Next() {
				if !hasEdges {
					sb.WriteString("**Вызывается из (Used by):**\n")
					hasEdges = true
				}
				var srcName, relation string
				if eRows.Scan(&srcName, &relation) == nil {
					sb.WriteString(fmt.Sprintf("- `%s` (%s)\n", srcName, relation))
				}
			}
			eRows.Close()
			if hasEdges {
				sb.WriteString("\n")
			}
		}

		// Dependencies (what this symbol calls)
		depsQuery := `
			SELECT target_name, relation
			FROM edges
			WHERE source_name = ?
		`
		dRows, err := p.store.db.QueryContext(ctx, depsQuery, name)
		if err == nil {
			hasDeps := false
			for dRows.Next() {
				if !hasDeps {
					sb.WriteString("**Зависит от (Calls/Uses):**\n")
					hasDeps = true
				}
				var tgtName, relation string
				if dRows.Scan(&tgtName, &relation) == nil {
					sb.WriteString(fmt.Sprintf("- `%s` (%s)\n", tgtName, relation))
				}
			}
			dRows.Close()
			if hasDeps {
				sb.WriteString("\n")
			}
		}
	}

	if !found {
		// Graceful Degradation: Find similar symbols using LIKE
		fuzzyQuery := `
			SELECT name, type, files.path 
			FROM nodes 
			JOIN files ON nodes.file_id = files.id
			WHERE name LIKE ? 
			LIMIT 5
		`
		fRows, err := p.store.db.QueryContext(ctx, fuzzyQuery, "%"+name+"%")
		if err == nil {
			defer fRows.Close()
			var suggestions []string
			for fRows.Next() {
				var sName, sType, sPath string
				if fRows.Scan(&sName, &sType, &sPath) == nil {
					suggestions = append(suggestions, fmt.Sprintf("- `%s` (%s в %s)", sName, sType, sPath))
				}
			}
			if len(suggestions) > 0 {
				return fmt.Sprintf("Символ '%s' не найден. Возможно, вы имели в виду один из этих символов:\n%s", name, strings.Join(suggestions, "\n")), nil
			}
		}

		return fmt.Sprintf("Символ '%s' не найден в базе (даже среди похожих). Проверьте правильность написания.", name), nil
	}

	return sb.String(), nil
}
