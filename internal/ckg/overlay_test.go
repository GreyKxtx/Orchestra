package ckg

import (
	"context"
	"strings"
	"testing"
)

func stagedTree(t *testing.T) (*Store, *Orchestrator, string) {
	t.Helper()
	root, store, orch := newIndexedTree(t)
	return store, orch, root
}

func overlayOf(t *testing.T, orch *Orchestrator, files ...StagedFile) (context.Context, func()) {
	t.Helper()
	byPath := map[string][]byte{}
	for _, f := range files {
		byPath[f.Path] = f.Content
	}
	octx, release, err := orch.OverlayContext(context.Background(), files, func(rel string) ([]byte, bool) {
		b, ok := byPath[rel]
		return b, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	return octx, release
}

// A symbol the turn wrote is in the graph the turn reads; one it removed is
// gone; the disk's graph is untouched (LLM-11). The store is one connection
// and the overlay holds it, so the disk is read before and after, never
// beside it.
func TestOverlayContext_TheGraphAnswersFromTheStagedFiles(t *testing.T) {
	store, orch, _ := stagedTree(t)
	ctx := context.Background()
	diskAlpha, _ := store.getNodeByFQN(ctx, "example.com/app.Alpha")
	if diskAlpha == nil || diskAlpha.LineStart != 3 {
		t.Fatalf("the disk's Alpha: %+v", diskAlpha)
	}
	if n, _ := store.getNodeByFQN(ctx, "example.com/app.Gamma"); n != nil {
		t.Fatalf("the disk's graph has no Gamma: %+v", n)
	}

	octx, release := overlayOf(t, orch,
		// Alpha moves down and calls the new Gamma, which Beta calls too.
		StagedFile{Path: "a.go", Content: []byte("package app\n\n// moved\n\nfunc Alpha() { Gamma() }\n\nfunc Gamma() {}\n")},
		// Beta no longer calls Alpha.
		StagedFile{Path: "b.go", Content: []byte("package app\n\nfunc Beta() { Gamma() }\n")},
	)
	gamma, err := store.getNodeByFQN(octx, "example.com/app.Gamma")
	if err != nil || gamma == nil || gamma.RelPath != "a.go" {
		t.Fatalf("the staged Gamma is in the turn's graph: %+v %v", gamma, err)
	}
	alpha, err := store.getNodeByFQN(octx, "example.com/app.Alpha")
	if err != nil || alpha == nil || alpha.LineStart != 5 {
		t.Fatalf("Alpha is where the staged file has it: %+v %v", alpha, err)
	}
	// Edges: Beta→Gamma resolves through the overlay (it dangled on disk),
	// Alpha→Gamma is the staged file's own, Beta→Alpha is gone.
	callers, err := store.neighbors(octx, "example.com/app.Gamma", false, []string{"calls"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, c := range callers {
		got[c.node.FQN] = true
	}
	if !got["example.com/app.Beta"] || !got["example.com/app.Alpha"] {
		t.Fatalf("Gamma's callers in the turn's graph: %v", got)
	}
	alphaCallers, err := store.neighbors(octx, "example.com/app.Alpha", false, []string{"calls"})
	if err != nil {
		t.Fatal(err)
	}
	if len(alphaCallers) != 0 {
		t.Fatalf("Beta no longer calls Alpha in the turn's graph: %+v", alphaCallers[0].node)
	}
	release()

	if n, _ := store.getNodeByFQN(ctx, "example.com/app.Gamma"); n != nil {
		t.Fatalf("the disk's graph still has no Gamma: %+v", n)
	}
	if n, _ := store.getNodeByFQN(ctx, "example.com/app.Alpha"); n == nil || n.LineStart != 3 {
		t.Fatalf("the disk's Alpha is unmoved: %+v", n)
	}
	if diskCallers, _ := store.neighbors(ctx, "example.com/app.Alpha", false, []string{"calls"}); len(diskCallers) != 1 {
		t.Fatalf("the disk's graph still has Beta→Alpha: %d", len(diskCallers))
	}
}

// An edge from a file the turn did not touch into one it did points at the
// staged node, with its staged position; the snippet is the staged source.
func TestOverlayContext_EdgesIntoAStagedFileAndSnippets(t *testing.T) {
	store, orch, root := stagedTree(t)
	octx, release := overlayOf(t, orch,
		StagedFile{Path: "a.go", Content: []byte("package app\n\n// moved\n\nfunc Alpha() { staged() }\n")},
	)
	callees, err := store.neighbors(octx, "example.com/app.Beta", true, []string{"calls"})
	if err != nil {
		t.Fatal(err)
	}
	var alpha *Node
	for _, c := range callees {
		if c.node.FQN == "example.com/app.Alpha" {
			alpha = c.node
		}
	}
	if alpha == nil || alpha.LineStart != 5 || alpha.RelPath != "a.go" {
		t.Fatalf("Beta's call reaches the staged Alpha: %+v", alpha)
	}
	out, err := NewProvider(store, root).ExploreSymbol(octx, "Alpha")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "staged()") {
		t.Fatalf("the snippet is the staged source:\n%s", out)
	}
	release()
	if out, _ := NewProvider(store, root).ExploreSymbol(context.Background(), "Alpha"); strings.Contains(out, "staged()") {
		t.Fatalf("the disk's snippet is the disk's:\n%s", out)
	}
}

// Release drops the temp schema before the connection returns to the pool:
// no later read sees the staged rows.
func TestOverlayContext_ReleaseLeavesThePoolClean(t *testing.T) {
	store, orch, _ := stagedTree(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		octx, release := overlayOf(t, orch, StagedFile{Path: "a.go", Content: []byte("package app\n\nfunc Alpha() {}\n\nfunc Gamma() {}\n")})
		if n, _ := store.getNodeByFQN(octx, "example.com/app.Gamma"); n == nil {
			t.Fatal("the overlay has Gamma")
		}
		release()
		if n, _ := store.getNodeByFQN(ctx, "example.com/app.Gamma"); n != nil {
			t.Fatalf("round %d: a released overlay's Gamma is still read: %+v", i, n)
		}
	}
	var temp int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_temp_master`).Scan(&temp); err != nil {
		t.Fatal(err)
	}
	if temp != 0 {
		t.Fatalf("temp objects left on a pooled connection: %d", temp)
	}
	// A file the parser does not know is no overlay at all.
	octx, release, err := orch.OverlayContext(ctx, []StagedFile{{Path: "notes.txt", Content: []byte("x")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if octx != ctx {
		t.Fatal("nothing to shadow, the context is the caller's")
	}
}
