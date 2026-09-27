package ckg

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The watcher (phase 7). Every explore and every agent run refreshes the
// graph; each one walked the tree to find out that nothing had changed.
// With the tree followed, a pass on an unchanged tree looks at nothing, and
// one after an edit stats that file alone.

// watchedTree indexes a tree, then follows it: the first pass under the
// watcher walks once (what changed before it started is unknown) and every
// later pass takes what the watcher names.
func watchedTree(t *testing.T) (string, *Store, *Orchestrator, *Watcher) {
	t.Helper()
	root, store, orch := newIndexedTree(t)
	w, err := Watch(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	store.SetChangeFeed(w)
	if err := orch.UpdateGraph(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !store.LastRefresh().Walked {
		t.Fatal("the first pass under a watcher must walk: it cannot know what moved before")
	}
	return root, store, orch, w
}

// waitNoted waits for the watcher to name every path in want.
func waitNoted(t *testing.T, w *Watcher, want ...string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		got := w.pendingPaths()
		missing := false
		for _, p := range want {
			if !slices.Contains(got, p) {
				missing = true
			}
		}
		if !missing {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("watcher noted %v, want %v", got, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWatcher_UnchangedTreeRefreshesWithoutAWalk(t *testing.T) {
	_, store, orch, _ := watchedTree(t)
	if err := orch.UpdateGraph(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := store.LastRefresh()
	if st.Walked || st.Seen != 0 || st.Hashed != 0 {
		t.Fatalf("pass on an unchanged watched tree: %+v, want no walk, nothing seen", st)
	}
}

func TestWatcher_ChangedFileIsTheOnlyOneLookedAt(t *testing.T) {
	root, store, orch, w := watchedTree(t)
	writeGoFile(t, root, "c.go", "package app\n\nfunc Gamma() {}\n")
	waitNoted(t, w, "c.go")
	if err := orch.UpdateGraph(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := store.LastRefresh()
	if st.Walked {
		t.Fatal("a pass the watcher fed must not walk")
	}
	if st.Seen != 1 || st.Parsed != 1 {
		t.Fatalf("pass after one new file: %+v, want seen 1, parsed 1", st)
	}
	if funcFQN(t, store, "Gamma") == "" {
		t.Fatal("Gamma is not in the graph")
	}
	// The dangling Beta→Gamma edge from the fixture resolves through the
	// same pass, like it does after a walk.
	if countCallEdges(t, store, "Beta", "Gamma") != 1 {
		t.Fatal("the call to Gamma was not relinked to the new node")
	}
	if got := w.pendingPaths(); len(got) != 0 {
		t.Fatalf("the pass landed, yet the watcher still names %v", got)
	}
}

func TestWatcher_RemovedFileLeavesTheGraph(t *testing.T) {
	root, store, orch, w := watchedTree(t)
	if err := os.Remove(filepath.Join(root, "a.go")); err != nil {
		t.Fatal(err)
	}
	waitNoted(t, w, "a.go")
	if err := orch.UpdateGraph(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := store.LastRefresh()
	if st.Walked || st.Deleted != 1 {
		t.Fatalf("pass after a removal: %+v, want no walk, deleted 1", st)
	}
	var n int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE short_name = 'Alpha'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("Alpha is still in the graph after its file was removed")
	}
}

func TestWatcher_RemovedDirectoryTakesItsFilesWithIt(t *testing.T) {
	root, store, orch, w := watchedTree(t)
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGoFile(t, root, "sub/d.go", "package sub\n\nfunc Delta() {}\n")
	waitNoted(t, w, "sub/d.go")
	if err := orch.UpdateGraph(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st := store.LastRefresh(); st.Walked || st.Parsed != 1 {
		t.Fatalf("pass after a file in a new directory: %+v, want no walk, parsed 1", st)
	}
	if err := os.RemoveAll(sub); err != nil {
		t.Fatal(err)
	}
	waitNoted(t, w, "sub")
	if err := orch.UpdateGraph(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st := store.LastRefresh(); st.Walked || st.Deleted != 1 {
		t.Fatalf("pass after the directory went: %+v, want no walk, deleted 1", st)
	}
}

func TestWatcher_FailedPassKeepsItsChangesForTheNext(t *testing.T) {
	root, store, orch, w := watchedTree(t)
	writeGoFile(t, root, "c.go", "package app\n\nfunc Gamma() {}\n")
	waitNoted(t, w, "c.go")
	// The store closes under the pass, after the scan took the watcher's
	// snapshot and before the graph is written: the pass fails, and what
	// the snapshot named is still owed.
	orch.afterScan = func() { _ = store.Close() }
	if err := orch.UpdateGraph(context.Background()); err == nil {
		t.Fatal("a pass whose writes failed must fail")
	}
	if got := w.pendingPaths(); !slices.Contains(got, "c.go") {
		t.Fatalf("the failed pass took c.go with it: watcher names %v", got)
	}
}

func TestWatcher_ChangeDuringAPassIsKeptForTheNext(t *testing.T) {
	_, _, _, w := watchedTree(t)
	// A pass takes its snapshot, a file changes while it runs, the pass
	// lands: the change must survive its ack.
	_, _, gen := w.Snapshot()
	w.mark("late.go")
	w.Ack(gen)
	if got := w.pendingPaths(); !slices.Contains(got, "late.go") {
		t.Fatalf("a change noted after the snapshot was acked away: %v", got)
	}
}

func TestWatcher_DroppedNotificationsOweAWalk(t *testing.T) {
	_, store, orch, w := watchedTree(t)
	// The kernel dropped notifications: what changed is unknown again.
	w.noteError(errOverflowForTest())
	if err := orch.UpdateGraph(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !store.LastRefresh().Walked {
		t.Fatal("after an overflow the next pass must walk")
	}
	if err := orch.UpdateGraph(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.LastRefresh().Walked {
		t.Fatal("the walk paid the overflow; the pass after it must not walk")
	}
}

func TestWatcher_OverflowDuringAPassSurvivesItsAck(t *testing.T) {
	_, _, _, w := watchedTree(t)
	_, full, gen := w.Snapshot()
	if full {
		t.Fatal("a watched tree after its first walk owes none")
	}
	w.noteError(errOverflowForTest())
	w.Ack(gen)
	if _, full, _ := w.Snapshot(); !full {
		t.Fatal("an overflow during a pass was acked away with that pass")
	}
}

func TestWatcher_ClosedWatcherOwesAWalk(t *testing.T) {
	_, store, orch, w := watchedTree(t)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := orch.UpdateGraph(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !store.LastRefresh().Walked {
		t.Fatal("a pass fed by a closed watcher must walk")
	}
}

func TestWatcher_IgnoredDirectoriesAreNotFollowed(t *testing.T) {
	root := t.TempDir()
	writeAppModule(t, root)
	for _, d := range []string{"node_modules/x", ".git/y", "gen/z"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	w, err := Watch(root, []string{"gen"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	for _, p := range w.fs.WatchList() {
		rel, _ := filepath.Rel(root, p)
		for _, bad := range []string{"node_modules", ".git", "gen"} {
			if rel == bad || strings.HasPrefix(rel, bad+string(filepath.Separator)) {
				t.Fatalf("%s is followed; it is ignored", rel)
			}
		}
	}
	if w.Dirs() != 1 {
		t.Fatalf("followed %d directories, want the root alone", w.Dirs())
	}
	// A file under an ignored directory is not a change of the graph.
	writeGoFile(t, root, "node_modules/x/m.go", "package x\n")
	writeGoFile(t, root, "k.go", "package app\n")
	waitNoted(t, w, "k.go")
	if got := w.pendingPaths(); slices.Contains(got, "node_modules/x/m.go") {
		t.Fatalf("a file under node_modules was noted: %v", got)
	}
}

func TestStore_WithoutAFeedEveryPassWalks(t *testing.T) {
	_, store, orch := newIndexedTree(t)
	if store.Watching() {
		t.Fatal("a store nobody watches says it is watched")
	}
	if err := orch.UpdateGraph(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !store.LastRefresh().Walked {
		t.Fatal("without a feed a pass must walk")
	}
}

func TestScanPaths_GonePathDropsWhatTheGraphHadUnderIt(t *testing.T) {
	root, store, orch := newIndexedTree(t)
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGoFile(t, root, "sub/d.go", "package sub\n\nfunc Delta() {}\n")
	writeGoFile(t, root, "sub/e.go", "package sub\n\nfunc Epsilon() {}\n")
	if err := orch.UpdateGraph(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The directory is moved out of the tree: the OS names the directory,
	// not the files it took with it.
	if err := os.Rename(sub, filepath.Join(t.TempDir(), "sub")); err != nil {
		t.Fatal(err)
	}
	res, err := orch.scanner.scanPaths(context.Background(), []string{"sub"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"sub/d.go", "sub/e.go"}; !slices.Equal(res.ToDelete, want) {
		t.Fatalf("ToDelete = %v, want %v", res.ToDelete, want)
	}
	if res.Walked {
		t.Fatal("a pass over named paths is not a walk")
	}
	_ = store
}
