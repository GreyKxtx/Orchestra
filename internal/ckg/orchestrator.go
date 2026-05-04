package ckg

import (
	"context"
	"path/filepath"
)

// Orchestrator ties together the Store, Scanner, and Parser to keep the
// Code Knowledge Graph up-to-date with the filesystem.
type Orchestrator struct {
	store      *Store
	scanner    *Scanner
	root       string
	modulePath string // Go module path (from go.mod)
	crateName  string // Rust crate name (from Cargo.toml)
}

// NewOrchestrator creates a new Orchestrator.
func NewOrchestrator(store *Store, root string) *Orchestrator {
	mp, _ := ParseModulePath(root)
	cn, _ := ParseCrateName(root)
	return &Orchestrator{
		store:      store,
		scanner:    NewScanner(store, root),
		root:       root,
		modulePath: mp,
		crateName:  cn,
	}
}

// modulePathFor returns the appropriate module identifier for the given file extension.
// For Rust files this is the crate name; for Go files it is the go.mod module path.
func (o *Orchestrator) modulePathFor(ext string) string {
	if ext == ".rs" {
		return o.crateName
	}
	return o.modulePath
}

// UpdateGraph runs an incremental scan and parses any changed files,
// updating the database transactionally.
func (o *Orchestrator) UpdateGraph(ctx context.Context) error {
	toParse, toDelete, err := o.scanner.Scan(ctx)
	if err != nil {
		return err
	}

	for _, relPath := range toDelete {
		if err := o.store.DeleteFile(ctx, relPath); err != nil {
			return err
		}
	}

	for _, relPath := range toParse {
		absPath := filepath.Join(o.root, filepath.FromSlash(relPath))
		ext := filepath.Ext(absPath)
		mp := o.modulePathFor(ext)

		nodes, edges, pkgName, err := ParseFile(ctx, mp, o.root, absPath)
		if err != nil {
			continue
		}

		hash, err := hashFile(absPath)
		if err != nil {
			continue
		}

		lang := LanguageFromExt(ext)
		if err := o.store.SaveFileNodes(ctx, relPath, hash, lang, mp, pkgName, nodes, edges); err != nil {
			return err
		}
	}

	return nil
}
