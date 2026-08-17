package ckg

import (
	"context"
	"path/filepath"
	"strings"
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
	return NewOrchestratorWithIgnores(store, root, nil)
}

// NewOrchestratorWithIgnores creates an Orchestrator whose scanner also
// applies project-configured exclude_dirs.
func NewOrchestratorWithIgnores(store *Store, root string, ignores []string) *Orchestrator {
	mp, _ := ParseModulePath(root)
	cn, _ := ParseCrateName(root)
	return &Orchestrator{
		store:      store,
		scanner:    NewScannerWithIgnores(store, root, ignores),
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
	if o == nil || o.store == nil {
		return nil
	}
	// Multiple entry points can refresh the same store (warmup, status,
	// rebuild, explore). Serialize them so the UI never observes a partial
	// file/language distribution.
	o.store.indexMu.Lock()
	defer o.store.indexMu.Unlock()
	return o.updateGraph(ctx)
}

// UpdateGraphAsync reserves the store update lock before returning, then runs
// the scan in a goroutine. Reserving synchronously closes the race where a
// status request could read stale/partial counters before warmup starts.
func (o *Orchestrator) UpdateGraphAsync(ctx context.Context) <-chan error {
	done := make(chan error, 1)
	if o == nil || o.store == nil {
		done <- nil
		close(done)
		return done
	}
	o.store.indexMu.Lock()
	go func() {
		defer o.store.indexMu.Unlock()
		defer close(done)
		done <- o.updateGraph(ctx)
	}()
	return done
}

func (o *Orchestrator) updateGraph(ctx context.Context) error {
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
		ext := strings.ToLower(filepath.Ext(absPath))
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

	// Always relink after the pass — including a single-file incremental
	// update — so new calls from B attach to already-indexed nodes in A.
	return o.store.RelinkUnresolvedEdges(ctx)
}
