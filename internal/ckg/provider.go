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

// ExploreSymbol is the single entry-point for the LLM explore tool.
// It automatically picks the right depth level based on the query shape:
//
//   - "internal/agent"    → package overview (structs, funcs, methods — no code bodies)
//   - "Agent"             → struct/interface definition + full method list
//   - "Agent.Run"         → full source code of the method/function + callers + callees
//   - "RecordSuccessfulCall" → finds CircuitBreaker.RecordSuccessfulCall via suffix search
func (p *Provider) ExploreSymbol(ctx context.Context, query string) (string, error) {
	// Package-level: query contains "/" but no "." after the last "/" segment.
	// e.g. "internal/agent" yes, "internal/agent.Agent" no.
	if isPackagePath(query) {
		return p.explorePackage(ctx, query)
	}

	type hit struct {
		id        int64
		fqn       string
		shortName string
		kind      string
		lineStart int
		lineEnd   int
		relPath   string
	}

	scanHits := func(rows *sql.Rows) ([]hit, error) {
		defer rows.Close()
		var out []hit
		for rows.Next() {
			var h hit
			if err := rows.Scan(&h.id, &h.fqn, &h.shortName, &h.kind, &h.lineStart, &h.lineEnd, &h.relPath); err != nil {
				return nil, fmt.Errorf("explore symbol: scan hit: %w", err)
			}
			out = append(out, h)
		}
		return out, rows.Err()
	}

	const q = `SELECT n.id, n.fqn, n.short_name, n.kind, n.line_start, n.line_end, f.path
	           FROM nodes n JOIN files f ON n.file_id = f.id WHERE `

	var hits []hit

	if IsLikelyFQN(query) {
		// 1. Exact FQN match.
		rows, err := p.store.db.QueryContext(ctx, q+`n.fqn = ?`, query)
		if err != nil {
			return "", err
		}
		if hits, err = scanHits(rows); err != nil {
			return "", err
		}

		// 2. FQN miss: model often writes "agent.Agent.Run" or "internal/agent.Agent.Run"
		//    instead of the stored full module path. Strip the package prefix and retry
		//    as short_name so the same paths as the non-FQN branch are tried.
		if len(hits) == 0 {
			// Strip everything up to and including the last "/" to get the local part
			// e.g. "internal/agent.Agent.Run" → "agent.Agent.Run"
			short := query
			if idx := strings.LastIndex(query, "/"); idx >= 0 {
				short = query[idx+1:]
			}
			// Extract last two dot-components: "agent.Agent.Run" → "Agent.Run"
			// This handles both "pkg.Type.Method" and "Type.Method" forms.
			parts := strings.Split(short, ".")
			switch {
			case len(parts) >= 2:
				short = parts[len(parts)-2] + "." + parts[len(parts)-1]
			default:
				short = parts[0]
			}
			// Try exact short_name match (e.g. "Agent.Run").
			rows2, err2 := p.store.db.QueryContext(ctx, q+`n.short_name = ?`, short)
			if err2 == nil {
				hits, _ = scanHits(rows2)
			}
			// For bare names (no dot) also try suffix search.
			if len(hits) == 0 && !strings.Contains(short, ".") {
				rows3, err3 := p.store.db.QueryContext(ctx, q+`n.short_name LIKE ?`, "%."+short)
				if err3 == nil {
					hits, _ = scanHits(rows3)
				}
			}
		}
	} else {
		// 1. Exact short_name match (e.g. "Agent.Run" or "Run").
		rows, err := p.store.db.QueryContext(ctx, q+`n.short_name = ?`, query)
		if err != nil {
			return "", err
		}
		if hits, err = scanHits(rows); err != nil {
			return "", err
		}

		// 2. Suffix match: find methods by unqualified name ("RecordSuccessfulCall"
		//    matches short_name "CircuitBreaker.RecordSuccessfulCall").
		if len(hits) == 0 {
			rows2, err := p.store.db.QueryContext(ctx, q+`n.short_name LIKE ?`, "%."+query)
			if err != nil {
				return "", err
			}
			if hits, err = scanHits(rows2); err != nil {
				return "", err
			}
		}
	}

	if len(hits) == 0 {
		return p.fuzzyFallback(ctx, query)
	}
	if len(hits) > 1 {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Запрос '%s' неоднозначен — найдено %d символов. Уточните:\n\n", query, len(hits)))
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
		sb.WriteString(fmt.Sprintf("```%s\n%s\n```\n", LanguageFromExt(ext), snippet))
		sb.WriteString("\n")

		// For structs/interfaces: list ALL methods so the model knows what's available.
		if h.kind == "struct" || h.kind == "interface" {
			prefix := h.shortName + "."
			mRows, mErr := p.store.db.QueryContext(ctx,
				`SELECT short_name, line_start, line_end FROM nodes
				 WHERE short_name LIKE ? AND file_id = (SELECT file_id FROM nodes WHERE id = ?)
				 ORDER BY line_start`,
				prefix+"%", h.id)
			if mErr == nil {
				var methods []struct{ sn string; ls, le int }
				for mRows.Next() {
					var sn string
					var ls2, le2 int
					if mRows.Scan(&sn, &ls2, &le2) == nil {
						methods = append(methods, struct{ sn string; ls, le int }{sn, ls2, le2})
					}
				}
				mRows.Close()
				if len(methods) > 0 {
					sb.WriteString("**Методы (explore нужный конкретный метод):**\n")
					for _, m := range methods {
						sb.WriteString(fmt.Sprintf("- `%s` (строки %d-%d)\n", m.sn, m.ls, m.le))
					}
					sb.WriteString("\n")
				}
			}
		}

		// Callers (capped to avoid overwhelming the model)
		cRows, err := p.store.db.QueryContext(ctx,
			`SELECT src.fqn, e.relation
			 FROM edges e
			 JOIN nodes src ON e.source_id = src.id
			 LEFT JOIN nodes tgt ON e.target_id = tgt.id
			 WHERE tgt.fqn = ? OR e.target_fqn = ?`, h.fqn, h.fqn)
		if err != nil {
			return "", fmt.Errorf("explore symbol: query callers: %w", err)
		}
		var callers []struct{ fqn, rel string }
		for cRows.Next() {
			var srcFQN, rel string
			if err := cRows.Scan(&srcFQN, &rel); err != nil {
				cRows.Close()
				return "", fmt.Errorf("explore symbol: scan caller: %w", err)
			}
			callers = append(callers, struct{ fqn, rel string }{srcFQN, rel})
		}
		if err := cRows.Err(); err != nil {
			cRows.Close()
			return "", fmt.Errorf("explore symbol: iterate callers: %w", err)
		}
		cRows.Close()
		if len(callers) > 0 {
			const maxCallers = 5
			sb.WriteString("**Вызывается из (callers):**\n")
			shown := callers
			if len(callers) > maxCallers {
				shown = callers[:maxCallers]
			}
			for _, c := range shown {
				sb.WriteString(fmt.Sprintf("- `%s` (%s)\n", c.fqn, c.rel))
			}
			if len(callers) > maxCallers {
				sb.WriteString(fmt.Sprintf("- ...и ещё %d callers\n", len(callers)-maxCallers))
			}
		}
		first := len(callers) == 0
		if !first {
			sb.WriteString("\n")
		}

		// Callees
		dRows, err := p.store.db.QueryContext(ctx,
			`SELECT e.target_fqn, e.relation FROM edges e WHERE e.source_id = ?`, h.id)
		if err != nil {
			return "", fmt.Errorf("explore symbol: query callees: %w", err)
		}
		first = true
		for dRows.Next() {
			if first {
				sb.WriteString("**Зависит от (callees):**\n")
				first = false
			}
			var tgtFQN, rel string
			if err := dRows.Scan(&tgtFQN, &rel); err != nil {
				dRows.Close()
				return "", fmt.Errorf("explore symbol: scan callee: %w", err)
			}
			sb.WriteString(fmt.Sprintf("- `%s` (%s)\n", tgtFQN, rel))
		}
		if err := dRows.Err(); err != nil {
			dRows.Close()
			return "", fmt.Errorf("explore symbol: iterate callees: %w", err)
		}
		dRows.Close()
		if !first {
			sb.WriteString("\n")
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
		if err := rows.Scan(&fqn, &kind, &path); err != nil {
			return "", fmt.Errorf("fuzzy fallback: scan suggestion: %w", err)
		}
		sugg = append(sugg, fmt.Sprintf("- `%s` (%s в %s)", fqn, kind, path))
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("fuzzy fallback: iterate suggestions: %w", err)
	}
	if len(sugg) == 0 {
		return fmt.Sprintf("Символ '%s' не найден в графе.", query), nil
	}
	return fmt.Sprintf("Символ '%s' не найден точно. Похожие:\n%s", query, strings.Join(sugg, "\n")), nil
}

// appendImportsSection appends a "### Зависимости" section to sb for pkgPath.
// Silently skips if no import data is available.
func (p *Provider) appendImportsSection(ctx context.Context, sb *strings.Builder, pkgPath string) {
	// Find the full package FQN from a package-kind node in the package files.
	row := p.store.db.QueryRowContext(ctx, `
		SELECT DISTINCT n.fqn FROM nodes n JOIN files f ON n.file_id = f.id
		WHERE n.kind = 'package'
		  AND f.path LIKE ?
		  AND f.path NOT LIKE ?
		LIMIT 1`,
		pkgPath+"/%",
		pkgPath+"/%/%",
	)
	var pkgFQN string
	if err := row.Scan(&pkgFQN); err != nil {
		return
	}

	// Outgoing imports: what this package imports.
	outRows, err := p.store.db.QueryContext(ctx, `
		SELECT DISTINCT e.target_fqn FROM edges e
		JOIN nodes n ON e.source_id = n.id
		WHERE n.fqn = ? AND e.relation = 'imports'
		ORDER BY e.target_fqn`,
		pkgFQN,
	)
	if err != nil {
		return
	}
	defer outRows.Close()
	var outImports []string
	for outRows.Next() {
		var s string
		if err := outRows.Scan(&s); err != nil {
			return
		}
		outImports = append(outImports, s)
	}
	if err := outRows.Err(); err != nil {
		return
	}

	// Incoming imports: who imports this package.
	importers, err := p.Importers(ctx, pkgFQN)
	if err != nil {
		importers = nil
	}

	if len(outImports) == 0 && len(importers) == 0 {
		return
	}

	sb.WriteString("### Зависимости\n")
	if len(outImports) > 0 {
		sb.WriteString("**Импортирует:**\n")
		for _, imp := range outImports {
			sb.WriteString(fmt.Sprintf("- `%s`\n", imp))
		}
	}
	if len(importers) > 0 {
		sb.WriteString("**Используется в:**\n")
		for _, imp := range importers {
			sb.WriteString(fmt.Sprintf("- `%s`\n", imp))
		}
	}
	sb.WriteString("\n")
}

// isPackagePath returns true when query looks like a directory/package path:
// contains "/" and the part after the last "/" has no "." (not a symbol FQN).
func isPackagePath(query string) bool {
	slash := strings.LastIndex(query, "/")
	if slash < 0 {
		return false
	}
	after := query[slash+1:]
	return !strings.Contains(after, ".")
}

// explorePackage returns a structured overview of all symbols in a package
// (matched by file path prefix) without loading any function bodies.
// Output groups types, exported functions, and methods by receiver.
func (p *Provider) explorePackage(ctx context.Context, pkgPath string) (string, error) {
	// Files whose relative path starts with pkgPath + "/" (direct children only, not sub-packages).
	rows, err := p.store.db.QueryContext(ctx, `
		SELECT f.path, n.fqn, n.short_name, n.kind, n.line_start, n.line_end
		FROM nodes n JOIN files f ON n.file_id = f.id
		WHERE f.path LIKE ?
		  AND f.path NOT LIKE ?
		  AND n.kind != 'package'
		ORDER BY f.path, n.line_start`,
		pkgPath+"/%",
		pkgPath+"/%/%", // exclude sub-packages
	)
	if err != nil {
		return "", fmt.Errorf("explore package: query: %w", err)
	}
	defer rows.Close()

	type sym struct {
		relPath   string
		fqn       string
		shortName string
		kind      string
		lineStart int
		lineEnd   int
	}
	var all []sym
	for rows.Next() {
		var s sym
		if err := rows.Scan(&s.relPath, &s.fqn, &s.shortName, &s.kind, &s.lineStart, &s.lineEnd); err != nil {
			return "", fmt.Errorf("explore package: scan: %w", err)
		}
		all = append(all, s)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("explore package: iterate: %w", err)
	}

	if len(all) == 0 {
		// Package may still be known (only a package-kind node, no symbols).
		// Try to show imports section before reporting "not found".
		var sb strings.Builder
		p.appendImportsSection(ctx, &sb, pkgPath)
		if sb.Len() > 0 {
			var out strings.Builder
			out.WriteString(fmt.Sprintf("## Пакет `%s` (нет символов)\n\n", pkgPath))
			out.WriteString(sb.String())
			out.WriteString("---\n")
			out.WriteString(fmt.Sprintf("→ Конкретный поиск: grep(\"паттерн\", paths=[\"%s\"])\n", pkgPath))
			return out.String(), nil
		}
		return fmt.Sprintf("Пакет '%s' не найден в графе или пуст.\nПоказать список известных пакетов: glob(\"**/*.go\").", pkgPath), nil
	}

	// Count unique files.
	fileSet := map[string]struct{}{}
	for _, s := range all {
		if !strings.HasSuffix(s.relPath, "_test.go") {
			fileSet[s.relPath] = struct{}{}
		}
	}

	// Separate: types (struct/interface), methods, funcs (exported/unexported).
	type typeEntry struct {
		s          sym
		methodCnt  int
		methodNames []string
	}
	var types []typeEntry
	// method receiver → method short names
	methodsByRecv := map[string][]string{}
	var exportedFuncs []sym
	var unexportedFuncs []sym

	// Collect types and index methods.
	typeIndex := map[string]*typeEntry{}
	for i := range all {
		s := all[i]
		if strings.HasSuffix(s.relPath, "_test.go") {
			continue
		}
		if s.kind == "struct" || s.kind == "interface" || s.kind == "type" {
			e := typeEntry{s: s}
			types = append(types, e)
			typeIndex[s.shortName] = &types[len(types)-1]
		}
	}
	for _, s := range all {
		if strings.HasSuffix(s.relPath, "_test.go") {
			continue
		}
		switch s.kind {
		case "method":
			// short_name = "Receiver.MethodName"
			dot := strings.Index(s.shortName, ".")
			if dot > 0 {
				recv := s.shortName[:dot]
				mname := s.shortName[dot+1:]
				methodsByRecv[recv] = append(methodsByRecv[recv], mname)
				if e, ok := typeIndex[recv]; ok {
					e.methodCnt++
					e.methodNames = append(e.methodNames, mname)
				}
			}
		case "func":
			if len(s.shortName) > 0 && s.shortName[0] >= 'A' && s.shortName[0] <= 'Z' {
				exportedFuncs = append(exportedFuncs, s)
			} else {
				unexportedFuncs = append(unexportedFuncs, s)
			}
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Пакет `%s` (%d файлов)\n\n", pkgPath, len(fileSet)))

	// --- Types ---
	if len(types) > 0 {
		sb.WriteString("### Типы (struct / interface)\n")
		for _, e := range types {
			line := fmt.Sprintf("- `%s` (%s, %s:%d-%d)",
				e.s.shortName, e.s.kind, filepath.Base(e.s.relPath), e.s.lineStart, e.s.lineEnd)
			if e.methodCnt > 0 {
				line += fmt.Sprintf(" — %d методов: %s", e.methodCnt, strings.Join(e.methodNames, ", "))
			}
			sb.WriteString(line + "\n")
		}
		sb.WriteString("\n")
	}

	// --- Exported funcs ---
	if len(exportedFuncs) > 0 {
		sb.WriteString("### Экспортируемые функции\n")
		for _, s := range exportedFuncs {
			sb.WriteString(fmt.Sprintf("- `%s` (%s:%d)\n", s.shortName, filepath.Base(s.relPath), s.lineStart))
		}
		sb.WriteString("\n")
	}

	// --- Unexported funcs (summarised per file) ---
	if len(unexportedFuncs) > 0 {
		byFile := map[string][]string{}
		for _, s := range unexportedFuncs {
			base := filepath.Base(s.relPath)
			byFile[base] = append(byFile[base], s.shortName)
		}
		sb.WriteString("### Внутренние функции (по файлам)\n")
		for file, names := range byFile {
			sb.WriteString(fmt.Sprintf("- **%s**: %s\n", file, strings.Join(names, ", ")))
		}
		sb.WriteString("\n")
	}

	// --- Orphan methods (receiver not in types list, e.g. defined in another file) ---
	for recv, methods := range methodsByRecv {
		if _, found := typeIndex[recv]; !found {
			sb.WriteString(fmt.Sprintf("### Методы `%s` (тип определён вне пакета или в другом файле)\n", recv))
			sb.WriteString("- " + strings.Join(methods, ", ") + "\n\n")
		}
	}

	// Зависимости (imports / imported-by).
	p.appendImportsSection(ctx, &sb, pkgPath)

	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("→ Детали типа: explore(\"Agent\") · Метод целиком: explore(\"Agent.Run\") · Конкретный поиск: grep(\"паттерн\", paths=[\"%s\"])\n", pkgPath))

	return sb.String(), nil
}

// Callers returns all nodes that have a "calls" or "instantiates" edge whose
// target_fqn equals the given fqn.
func (p *Provider) Callers(ctx context.Context, fqn string) ([]Node, error) {
	rows, err := p.store.db.QueryContext(ctx, `
        SELECT n.id, n.file_id, n.fqn, n.short_name, n.kind, n.line_start, n.line_end, n.complexity
        FROM edges e
		JOIN nodes n ON e.source_id = n.id
		LEFT JOIN nodes t ON e.target_id = t.id
        WHERE (t.fqn = ? OR e.target_fqn = ?)
		  AND e.relation IN ('calls','instantiates')`, fqn, fqn)
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
	if err := rows.Err(); err != nil {
		return nil, err
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
	if err := rows.Err(); err != nil {
		return nil, err
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
