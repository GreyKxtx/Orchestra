package ckg

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// ChangeFeed is how a scanner learns what changed since the last pass
// without walking the tree. Snapshot names the paths that changed and says
// whether a walk is owed anyway; Ack, called once the pass that took the
// snapshot has committed, drops what that snapshot covered. A change made
// after the snapshot is kept for the next pass, and a pass that fails acks
// nothing, so a change is never lost to a pass that did not land it.
type ChangeFeed interface {
	Snapshot() (paths []string, full bool, gen uint64)
	Ack(gen uint64)
}

// maxWatchedDirs bounds the directories one Watcher follows. A tree past it
// is left to the walk: the kernel's own limit on watches is near, and the
// walk on a tree that size is what the scanner does anyway.
const maxWatchedDirs = 32768

// errTooManyDirs is why Watch gives up on a very large tree.
var errTooManyDirs = fmt.Errorf("more than %d directories", maxWatchedDirs)

// Watcher follows the workspace through the OS's file notifications and
// feeds the scanner the paths that changed, so a refresh on a tree nothing
// touched costs no walk, and one after an edit stats that file alone. Every
// explore and every agent run refreshes the graph; without the watcher each
// one walked the tree (45 ms on Orchestra's own, more on a large one).
//
// It owes the scanner a walk when it cannot know what changed: at the start
// (what moved before it was watching), after the kernel dropped
// notifications (overflow), when a new directory could not be followed, and
// once closed. The walk it owes is the one the scanner already knows how to
// do, so the graph is right either way; the watcher only makes it cheap.
type Watcher struct {
	fs     *fsnotify.Watcher
	root   string
	ignore func(abs string) bool

	mu sync.Mutex
	// pending maps a changed path (slash-separated, relative to root) to the
	// generation it was noted in; Ack(gen) drops the ones noted at or before
	// gen. A path may be a directory: the scanner then looks under it.
	pending map[string]uint64
	gen     uint64
	// full says a walk is owed; fullGen is the generation it became owed in,
	// so an Ack of an earlier snapshot does not clear it.
	full    bool
	fullGen uint64
	dirs    int

	done chan struct{}
}

// Watch starts following root, minus the scanner's ignores (built-in,
// exclude_dirs, .gitignore, .orchestraignore). It fails on a tree the OS
// cannot follow — too many directories for the watch limit, a platform with
// no notifications — and the caller then keeps the walk.
func Watch(root string, ignores []string) (*Watcher, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{
		fs:      fsw,
		root:    abs,
		ignore:  NewScannerWithIgnores(nil, abs, ignores).isIgnored,
		pending: map[string]uint64{},
		full:    true,
		done:    make(chan struct{}),
	}
	if err := w.addTree(abs, false); err != nil {
		_ = fsw.Close()
		return nil, err
	}
	go w.loop()
	return w, nil
}

// Close stops following the tree. A feed that stopped owes a walk to any
// pass that still consults it.
func (w *Watcher) Close() error {
	if w == nil {
		return nil
	}
	w.owe()
	err := w.fs.Close()
	<-w.done
	return err
}

// Dirs is how many directories are followed.
func (w *Watcher) Dirs() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.dirs
}

// Snapshot implements ChangeFeed.
func (w *Watcher) Snapshot() ([]string, bool, uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	paths := make([]string, 0, len(w.pending))
	for p := range w.pending {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	gen := w.gen
	w.gen++
	return paths, w.full, gen
}

// Ack implements ChangeFeed.
func (w *Watcher) Ack(gen uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for p, g := range w.pending {
		if g <= gen {
			delete(w.pending, p)
		}
	}
	if w.full && w.fullGen <= gen {
		w.full = false
	}
}

// owe records that the next pass has to walk: a Snapshot taken earlier
// cannot clear it.
func (w *Watcher) owe() {
	w.mu.Lock()
	w.full = true
	w.fullGen = w.gen
	w.mu.Unlock()
}

func (w *Watcher) mark(rel string) {
	w.mu.Lock()
	w.pending[rel] = w.gen
	w.mu.Unlock()
}

// addTree follows dir and every directory under it that is not ignored.
// With markFiles, the indexable files it finds are noted as changed: a
// directory that appeared whole (a move, an unpack) carries files no
// notification will name.
func (w *Watcher) addTree(dir string, markFiles bool) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == dir {
				return walkErr
			}
			return nil
		}
		if path != w.root && w.ignore(path) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			w.mu.Lock()
			w.dirs++
			n := w.dirs
			w.mu.Unlock()
			if n > maxWatchedDirs {
				return errTooManyDirs
			}
			if err := w.fs.Add(path); err != nil {
				return fmt.Errorf("watch %s: %w", path, err)
			}
			return nil
		}
		if markFiles && Indexable(filepath.Ext(path)) {
			if rel, ok := w.relOf(path); ok {
				w.mark(rel)
			}
		}
		return nil
	})
}

func (w *Watcher) relOf(abs string) (string, bool) {
	rel, err := filepath.Rel(w.root, abs)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func (w *Watcher) loop() {
	defer close(w.done)
	for {
		select {
		case ev, ok := <-w.fs.Events:
			if !ok {
				return
			}
			w.handle(ev)
		case err, ok := <-w.fs.Errors:
			if !ok {
				return
			}
			w.noteError(err)
		}
	}
}

// noteError is the Errors channel: an overflow means notifications were
// dropped, so a walk is owed; anything else is the OS's business.
func (w *Watcher) noteError(err error) {
	if errors.Is(err, fsnotify.ErrEventOverflow) {
		w.owe()
	}
}

// handle notes one notification. A directory that appeared is followed and
// its files noted; anything else is noted by path — a path that turns out
// to be gone makes the scanner drop what the graph had under it, so a
// directory removed or renamed away needs no special case here.
func (w *Watcher) handle(ev fsnotify.Event) {
	rel, ok := w.relOf(ev.Name)
	if !ok || w.ignore(ev.Name) {
		return
	}
	if ev.Has(fsnotify.Create) {
		if info, err := os.Lstat(ev.Name); err == nil && info.IsDir() {
			if err := w.addTree(ev.Name, true); err != nil {
				w.owe()
			}
			return
		}
	}
	if ev.Has(fsnotify.Create | fsnotify.Write | fsnotify.Remove | fsnotify.Rename | fsnotify.Chmod) {
		w.mark(rel)
	}
}

// pendingPaths is what the next Snapshot would name, without taking one.
func (w *Watcher) pendingPaths() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]string, 0, len(w.pending))
	for p := range w.pending {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
