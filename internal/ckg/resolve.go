package ckg

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// queryer is implemented by *sql.DB and *sql.Tx.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func resolveEdgeTarget(ctx context.Context, q queryer, selByFQN *sql.Stmt, rawTarget, relation, srcPkg string) (*int64, string, error) {
	var tid int64
	var err error
	if selByFQN != nil {
		err = selByFQN.QueryRowContext(ctx, rawTarget).Scan(&tid)
	} else {
		err = q.QueryRowContext(ctx, `SELECT id FROM nodes WHERE fqn = ?`, rawTarget).Scan(&tid)
	}
	if err == nil {
		return &tid, rawTarget, nil
	}
	if err != sql.ErrNoRows {
		return nil, "", fmt.Errorf("resolve target %s: %w", rawTarget, err)
	}

	if relation != "calls" && relation != "instantiates" {
		return nil, rawTarget, nil
	}
	// Module-qualified or rust/ts FQN: leave dangling until exact node exists.
	if strings.Contains(rawTarget, "/") || strings.Contains(rawTarget, "::") {
		return nil, rawTarget, nil
	}

	short := rawTarget
	pkgHint := srcPkg
	if i := strings.LastIndex(rawTarget, "."); i > 0 && !strings.Contains(rawTarget[:i], "/") {
		pkgHint = rawTarget[:i]
		short = rawTarget[i+1:]
	}

	if pkgHint != "" {
		id, fqn, ok, err := uniqueNodeByPackageShort(ctx, q, pkgHint, short)
		if err != nil {
			return nil, "", err
		}
		if ok {
			return &id, fqn, nil
		}
	}
	id, fqn, ok, err := uniqueNodeByShort(ctx, q, short)
	if err != nil {
		return nil, "", err
	}
	if ok {
		return &id, fqn, nil
	}
	return nil, rawTarget, nil
}

func uniqueNodeByPackageShort(ctx context.Context, q queryer, pkg, short string) (int64, string, bool, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, fqn FROM nodes WHERE package = ? AND (short_name = ? OR short_name LIKE ?) LIMIT 2`,
		pkg, short, "%."+short)
	if err != nil {
		return 0, "", false, fmt.Errorf("resolve package short %s.%s: %w", pkg, short, err)
	}
	defer rows.Close()
	return scanUniqueNode(rows, pkg+"."+short)
}

func uniqueNodeByShort(ctx context.Context, q queryer, short string) (int64, string, bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, fqn FROM nodes WHERE short_name = ? LIMIT 2`, short)
	if err != nil {
		return 0, "", false, fmt.Errorf("resolve short_name %s: %w", short, err)
	}
	defer rows.Close()
	return scanUniqueNode(rows, short)
}

func scanUniqueNode(rows *sql.Rows, label string) (int64, string, bool, error) {
	type candidate struct {
		id  int64
		fqn string
	}
	var cands []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.fqn); err != nil {
			return 0, "", false, fmt.Errorf("scan candidate %s: %w", label, err)
		}
		cands = append(cands, c)
	}
	if err := rows.Err(); err != nil {
		return 0, "", false, fmt.Errorf("iterate candidates %s: %w", label, err)
	}
	if len(cands) == 1 {
		return cands[0].id, cands[0].fqn, true, nil
	}
	return 0, "", false, nil
}

func isExternalTarget(fqn, modulePath string) bool {
	fqn = strings.TrimSpace(fqn)
	if fqn == "" {
		return false
	}
	if modulePath != "" && (fqn == modulePath ||
		strings.HasPrefix(fqn, modulePath+".") ||
		strings.HasPrefix(fqn, modulePath+"/")) {
		return false
	}
	if goBuiltins[fqn] {
		return true
	}
	first, _, hasSlash := strings.Cut(fqn, "/")
	if hasSlash && !strings.Contains(first, ".") {
		return true
	}
	if !hasSlash {
		pkg, _, ok := strings.Cut(fqn, ".")
		if ok && stdlibRoot[pkg] {
			return true
		}
	}
	return false
}

// RelinkUnresolvedEdges fills target_id / canonical target_fqn for dangling
// calls/instantiates after an index pass. Safe under indexMu write lock.
func (s *Store) RelinkUnresolvedEdges(ctx context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.target_fqn, e.relation, COALESCE(src.package, '')
		FROM edges e
		JOIN nodes src ON src.id = e.source_id
		WHERE e.target_id IS NULL AND e.relation IN ('calls', 'instantiates')`)
	if err != nil {
		return fmt.Errorf("relink: list dangling: %w", err)
	}
	type dang struct {
		id  int64
		tgt string
		rel string
		pkg string
	}
	var pending []dang
	for rows.Next() {
		var d dang
		if err := rows.Scan(&d.id, &d.tgt, &d.rel, &d.pkg); err != nil {
			rows.Close()
			return fmt.Errorf("relink: scan: %w", err)
		}
		pending = append(pending, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}

	upd, err := s.db.PrepareContext(ctx, `UPDATE edges SET target_id = ?, target_fqn = ?, is_external = 0 WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("relink: prepare: %w", err)
	}
	defer upd.Close()

	for _, d := range pending {
		tid, canon, err := resolveEdgeTarget(ctx, s.db, nil, d.tgt, d.rel, d.pkg)
		if err != nil || tid == nil {
			continue
		}
		if _, err := upd.ExecContext(ctx, tid, canon, d.id); err != nil {
			return fmt.Errorf("relink: update %d: %w", d.id, err)
		}
	}
	return nil
}

var goBuiltins = map[string]bool{
	"append": true, "cap": true, "clear": true, "close": true, "complex": true,
	"copy": true, "delete": true, "imag": true, "len": true, "make": true,
	"max": true, "min": true, "new": true, "panic": true, "print": true,
	"println": true, "real": true, "recover": true,
}

var stdlibRoot = map[string]bool{
	"archive": true, "bufio": true, "bytes": true, "cmp": true, "compress": true,
	"container": true, "context": true, "crypto": true, "database": true,
	"debug": true, "embed": true, "encoding": true, "errors": true, "expvar": true,
	"flag": true, "fmt": true, "go": true, "hash": true, "html": true, "image": true,
	"index": true, "io": true, "iter": true, "log": true, "maps": true, "math": true,
	"mime": true, "net": true, "os": true, "path": true, "plugin": true,
	"reflect": true, "regexp": true, "runtime": true, "slices": true, "sort": true,
	"strconv": true, "strings": true, "sync": true, "syscall": true, "testing": true,
	"text": true, "time": true, "unicode": true, "unsafe": true,
}
