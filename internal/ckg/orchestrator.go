package ckg

import (
	"context"
	"path/filepath"
)

// Orchestrator ties together the Store, Scanner, and Parser to keep the
// Code Knowledge Graph up-to-date with the filesystem.
type Orchestrator struct {
	store   *Store
	scanner *Scanner
	root    string
}

// NewOrchestrator creates a new Orchestrator.
func NewOrchestrator(store *Store, root string) *Orchestrator {
	return &Orchestrator{
		store:   store,
		scanner: NewScanner(store, root),
		root:    root,
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

		// Extract AST structural nodes
		nodes, edges, err := ParseFile(ctx, absPath)
		if err != nil {
			// In a real codebase, some files might have syntax errors (e.g. while being typed).
			// We skip files that cannot be parsed so we don't halt the entire indexing process.
			continue
		}

		// Compute hash again since the scanner just returns the paths.
		hash, err := hashFile(absPath)
		if err != nil {
			continue
		}

		lang := LanguageFromExt(filepath.Ext(absPath))

		// Save the file and its nodes transactionally
		if err := o.store.SaveFileNodes(ctx, relPath, hash, lang, nodes, edges); err != nil {
			return err
		}
	}

	return nil
}
