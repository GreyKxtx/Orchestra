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
	modulePath string
}

// NewOrchestrator creates a new Orchestrator.
func NewOrchestrator(store *Store, root string) *Orchestrator {
	mp, _ := ParseModulePath(root) // empty string for non-Go workspaces — safe
	return &Orchestrator{
		store:      store,
		scanner:    NewScanner(store, root),
		root:       root,
		modulePath: mp,
	}
}

// UpdateGraph runs an incremental scan and parses any changed Go files,
// updating the database transactionally.
func (o *Orchestrator) UpdateGraph(ctx context.Context) error {
	toParse, toDelete, err := o.scanner.Scan(ctx)
	if err != nil {
		return err
	}

	// 1. Delete files that no longer exist
	for _, relPath := range toDelete {
		if err := o.store.DeleteFile(ctx, relPath); err != nil {
			return err
		}
	}

	// 2. Parse and save new/modified files
	for _, relPath := range toParse {
		absPath := filepath.Join(o.root, filepath.FromSlash(relPath))

		nodes, edges, pkgName, err := ParseFile(ctx, o.modulePath, o.root, absPath)
		if err != nil {
			// Files with syntax errors are skipped to keep indexing resilient.
			continue
		}

		hash, err := hashFile(absPath)
		if err != nil {
			continue
		}

		lang := LanguageFromExt(filepath.Ext(absPath))

		if err := o.store.SaveFileNodes(ctx, relPath, hash, lang, o.modulePath, pkgName, nodes, edges); err != nil {
			return err
		}
	}

	return nil
}
