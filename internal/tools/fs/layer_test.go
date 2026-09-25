package fs

import (
	"errors"
	iofs "io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/patch/cache"
)

func layerRoot(t *testing.T) (*Overlay, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("disk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return NewOverlay(root, OverlayOptions{DryRun: true}), root
}

func stage(t *testing.T, o *Overlay, path, content string) {
	t.Helper()
	if err := o.stageFile(nil, path, content, cache.ComputeSHA256([]byte(content))); err != nil {
		t.Fatal(err)
	}
}

func content(o *Overlay, path string) string {
	c, _ := o.currentContent(nil, path)
	return c
}

// A layer reads through to its owner live and keeps its writes to itself
// until it commits.
func TestLayer_ReadsThroughAndCommits(t *testing.T) {
	turn, _ := layerRoot(t)
	task := turn.Fork()
	if content(task, "a.go") != "disk\n" {
		t.Fatal("a layer reads the disk through its owner")
	}
	stage(t, turn, "b.go", "turn\n")
	if content(task, "b.go") != "turn\n" {
		t.Fatal("a layer sees what its owner stages after it forked")
	}
	stage(t, task, "a.go", "task\n")
	if content(turn, "a.go") != "disk\n" {
		t.Fatal("the owner must not see an uncommitted layer")
	}
	if got := task.StagedFileContent(); got["a.go"] != "task\n" || got["b.go"] != "turn\n" {
		t.Fatalf("the layer's view is its owner's with its own on top: %v", got)
	}
	paths, err := task.Commit()
	if err != nil || len(paths) != 1 || paths[0] != "a.go" {
		t.Fatalf("commit: %v %v", paths, err)
	}
	if content(turn, "a.go") != "task\n" {
		t.Fatal("a committed layer's change is its owner's")
	}
	ops := turn.StagedOps()
	for _, op := range ops {
		if op.Path == "a.go" && op.WriteAtomic.Conditions.FileHash != cache.ComputeSHA256([]byte("disk\n")) {
			t.Error("the merged op still guards against the disk version it was made from")
		}
	}
}

func TestLayer_DiscardLeavesNothing(t *testing.T) {
	turn, _ := layerRoot(t)
	task := turn.Fork()
	stage(t, task, "a.go", "broken\n")
	task.Discard()
	if content(turn, "a.go") != "disk\n" || turn.HasStagedChanges() {
		t.Fatal("a discarded layer leaves the owner as it was")
	}
	if _, err := task.Commit(); !errors.Is(err, ErrLayerDiscarded) {
		t.Fatalf("a discarded layer cannot commit: %v", err)
	}
}

// Two tasks change one file from the same version: the first to commit wins,
// the second is a conflict and changes nothing.
func TestLayer_ConcurrentChangeIsAConflict(t *testing.T) {
	turn, _ := layerRoot(t)
	first, second := turn.Fork(), turn.Fork()
	stage(t, first, "a.go", "first\n")
	stage(t, second, "a.go", "second\n")
	stage(t, second, "c.go", "only second\n")
	if _, err := first.Commit(); err != nil {
		t.Fatal(err)
	}
	_, err := second.Commit()
	var conflict *MergeConflict
	if !errors.As(err, &conflict) || len(conflict.Paths) != 1 || conflict.Paths[0] != "a.go" {
		t.Fatalf("want a conflict on a.go, got %v", err)
	}
	if content(turn, "a.go") != "first\n" {
		t.Fatal("the conflicting layer must not overwrite the first")
	}
	if _, staged, _ := turn.stagedContent("c.go"); staged != "" {
		t.Fatal("a conflict commits nothing, not even the files that did not conflict")
	}

	// The same change twice is no conflict.
	a, b := turn.Fork(), turn.Fork()
	stage(t, a, "d.go", "same\n")
	stage(t, b, "d.go", "same\n")
	if _, err := a.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Commit(); err != nil {
		t.Fatalf("an identical change is not a conflict: %v", err)
	}
}

// A Lead's workers commit into the Lead's layer, and the Lead's commit carries
// them up. If the Lead is discarded, so is what its workers gave it.
func TestLayer_Nested(t *testing.T) {
	turn, _ := layerRoot(t)
	lead := turn.Fork()
	worker := lead.Fork()
	stage(t, worker, "a.go", "worker\n")
	if _, err := worker.Commit(); err != nil {
		t.Fatal(err)
	}
	if content(turn, "a.go") != "disk\n" || content(lead, "a.go") != "worker\n" {
		t.Fatal("a worker's commit goes to its Lead, not past it")
	}
	// A sibling of the Lead changes a.go at the turn meanwhile: the Lead's
	// commit is checked against the version the worker started from.
	other := turn.Fork()
	stage(t, other, "a.go", "other\n")
	if _, err := other.Commit(); err != nil {
		t.Fatal(err)
	}
	var conflict *MergeConflict
	if _, err := lead.Commit(); !errors.As(err, &conflict) {
		t.Fatalf("the Lead's commit must see the worker's base: %v", err)
	}

	lead2 := turn.Fork()
	late := lead2.Fork()
	stage(t, late, "e.go", "late\n")
	lead2.Discard()
	if _, err := late.Commit(); !errors.Is(err, ErrLayerDiscarded) {
		t.Fatalf("a worker of a discarded Lead cannot commit: %v", err)
	}
	if _, staged, _ := turn.stagedContent("e.go"); staged != "" {
		t.Fatal("nothing of a discarded Lead reaches the turn")
	}
}

// A task forked from a Lead that already committed works on the Lead's owner,
// and its commit goes there.
func TestLayer_ForkAndCommitPastAMergedLayer(t *testing.T) {
	turn, _ := layerRoot(t)
	lead := turn.Fork()
	stage(t, lead, "spec.md", "spec\n")
	worker := lead.Fork()
	if _, err := lead.Commit(); err != nil {
		t.Fatal(err)
	}
	relayed := lead.Fork()
	if relayed.parent != turn {
		t.Fatal("a fork of a merged layer is a fork of its owner")
	}
	if content(relayed, "spec.md") != "spec\n" {
		t.Fatal("the Lead's committed spec is visible")
	}
	stage(t, worker, "a.go", "worker\n")
	if _, err := worker.Commit(); err != nil {
		t.Fatal(err)
	}
	if content(turn, "a.go") != "worker\n" {
		t.Fatal("a commit into a merged layer goes on to its owner")
	}
}

func TestLayer_NoLayerOutsideADryRun(t *testing.T) {
	o := NewOverlay(t.TempDir(), OverlayOptions{DryRun: false})
	if o.Fork() != nil {
		t.Fatal("writes go to disk outside a dry run; there is nothing to layer")
	}
}

// ReadFile is the view the runtime's checks get: a task's own changes over
// its owner's over the disk, and ErrNotExist for a file in none of them.
func TestLayer_ReadFileIsTheTasksView(t *testing.T) {
	turn, _ := layerRoot(t)
	task := turn.Fork()
	stage(t, turn, "brief.md", "turn\n")
	stage(t, task, "a.go", "task\n")
	for path, want := range map[string]string{"a.go": "task\n", "./brief.md": "turn\n"} {
		got, err := task.ReadFile(path)
		if err != nil || string(got) != want {
			t.Errorf("task reads %s = %q, %v; want %q", path, got, err, want)
		}
	}
	if got, _ := turn.ReadFile("a.go"); string(got) != "disk\n" {
		t.Errorf("the owner does not see the task's change: %q", got)
	}
	if _, err := task.ReadFile("missing.md"); !errors.Is(err, iofs.ErrNotExist) {
		t.Errorf("a missing file is ErrNotExist: %v", err)
	}
}

// A checkpoint's staged files come back as they were, disk version included:
// the final op still guards against the version the edit was made from.
func TestOverlay_SnapshotRestore(t *testing.T) {
	turn, root := layerRoot(t)
	stage(t, turn, "a.go", "edited\n")
	stage(t, turn, "new.go", "fresh\n")
	snap := turn.SnapshotStaged()
	if len(snap) != 2 || snap[0].Path != "a.go" || snap[0].DiskHash != cache.ComputeSHA256([]byte("disk\n")) || !snap[1].IsNew {
		t.Fatalf("snapshot: %+v", snap)
	}

	fresh := NewOverlay(root, OverlayOptions{DryRun: true})
	fresh.RestoreStaged(snap)
	if content(fresh, "a.go") != "edited\n" || content(fresh, "new.go") != "fresh\n" {
		t.Fatal("restored content")
	}
	for _, op := range fresh.StagedOps() {
		if op.Path == "a.go" && op.WriteAtomic.Conditions.FileHash != cache.ComputeSHA256([]byte("disk\n")) {
			t.Errorf("the restored op guards against the disk version the edit was made from: %+v", op.WriteAtomic.Conditions)
		}
	}
}
