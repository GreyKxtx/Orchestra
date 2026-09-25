package ckg

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
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
//
// Refreshes are serialized with each other (refreshMu). The walk and the
// parsing run without the graph's write lock; readers are held out only
// while the changed files are written and relinked, and not at all on an
// unchanged tree (DATA-3: every explore and every child agent used to wait
// for a full scan under the lock).
func (o *Orchestrator) UpdateGraph(ctx context.Context) error {
	if o == nil || o.store == nil {
		return nil
	}
	o.store.refreshMu.Lock()
	defer o.store.refreshMu.Unlock()
	return o.updateGraph(ctx, false)
}

// UpdateGraphAsync reserves the store's locks before returning, then runs
// the scan in a goroutine. Reserving synchronously closes the race where a
// status request could read stale/partial counters before warmup starts.
func (o *Orchestrator) UpdateGraphAsync(ctx context.Context) <-chan error {
	done := make(chan error, 1)
	if o == nil || o.store == nil {
		done <- nil
		close(done)
		return done
	}
	o.store.refreshMu.Lock()
	o.store.indexMu.Lock()
	go func() {
		defer o.store.refreshMu.Unlock()
		defer o.store.indexMu.Unlock()
		defer close(done)
		done <- o.updateGraph(ctx, true)
	}()
	return done
}

// RefreshInBackground starts UpdateGraph in a goroutine unless a background
// pass is already running, and returns at once: the caller reads the graph
// as it is now. Step-1 context is fetched this way — once per agent run,
// children included — so a run no longer pays a scan before its first step.
func (o *Orchestrator) RefreshInBackground(ctx context.Context) {
	if o == nil || o.store == nil {
		return
	}
	if !o.store.refreshing.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer o.store.refreshing.Store(false)
		_ = o.UpdateGraph(ctx)
	}()
}

// parsedFile is one changed file, parsed before the write lock is taken.
type parsedFile struct {
	rel     string
	stamp   FileStamp
	lang    string
	module  string
	pkgName string
	nodes   []Node
	edges   []Edge
}

// updateGraph is one pass. Called with refreshMu held; with locked, the
// caller holds indexMu for the whole pass (the warmup does, so index.status
// never reads a half-built graph), otherwise it is taken for the writes only.
func (o *Orchestrator) updateGraph(ctx context.Context, locked bool) error {
	start := time.Now()
	var st RefreshStats

	if !locked {
		o.store.indexMu.RLock()
	}
	changes, err := o.scanner.ScanChanges(ctx)
	if !locked {
		o.store.indexMu.RUnlock()
	}
	if err != nil {
		return err
	}
	st.Seen = len(changes.Stamps)
	st.Hashed = changes.Hashed

	var parsed []parsedFile
	for _, relPath := range changes.ToParse {
		absPath := filepath.Join(o.root, filepath.FromSlash(relPath))
		ext := strings.ToLower(filepath.Ext(absPath))
		mp := o.modulePathFor(ext)

		nodes, edges, pkgName, err := ParseFile(ctx, mp, o.root, absPath)
		if err != nil {
			continue
		}
		// The stamp is of the content that was parsed: a file that changes
		// between the walk and here is stamped anew and parsed again next pass.
		stamp, err := stampFile(absPath)
		if err != nil {
			continue
		}
		parsed = append(parsed, parsedFile{
			rel: relPath, stamp: stamp, lang: LanguageFromExt(ext), module: mp, pkgName: pkgName,
			nodes: nodes, edges: edges,
		})
	}

	if len(parsed) == 0 && len(changes.ToDelete) == 0 && len(changes.Restamp) == 0 {
		st.Elapsed = time.Since(start)
		o.store.setLastRefresh(st)
		return nil
	}

	lockedAt := time.Now()
	if !locked {
		o.store.indexMu.Lock()
		defer o.store.indexMu.Unlock()
	}
	if o.store.db == nil {
		return errStoreClosed
	}
	defer func() {
		st.Locked = time.Since(lockedAt)
		st.Elapsed = time.Since(start)
		o.store.setLastRefresh(st)
	}()

	for _, relPath := range changes.ToDelete {
		if err := o.store.DeleteFile(ctx, relPath); err != nil {
			return err
		}
		st.Deleted++
	}

	var inserted []Node
	for _, f := range parsed {
		if err := o.store.SaveFileNodesStamped(ctx, f.rel, f.stamp, f.lang, f.module, f.pkgName, f.nodes, f.edges); err != nil {
			return err
		}
		st.Parsed++
		inserted = append(inserted, f.nodes...)
	}
	if err := o.store.restampFiles(ctx, changes.Restamp); err != nil {
		return err
	}

	// Relink after the pass — including a single-file incremental update —
	// so old calls to a symbol this pass (re)indexed attach to it. Only the
	// edges that name one of the inserted nodes are looked at.
	candidates, relinked, err := o.store.RelinkEdgesTo(ctx, inserted)
	st.Candidates, st.Relinked = candidates, relinked
	return err
}

// stampFile hashes the file and records the mtime and size it had.
func stampFile(absPath string) (FileStamp, error) {
	hash, err := hashFile(absPath)
	if err != nil {
		return FileStamp{}, err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return FileStamp{}, err
	}
	return FileStamp{Hash: hash, MTimeNS: info.ModTime().UnixNano(), Size: info.Size()}, nil
}
