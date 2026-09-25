package fs

import (
	"context"
	"testing"

	"github.com/orchestra/orchestra/patch/fsutil"
	"github.com/orchestra/orchestra/protocol"
)

// LLM-13: an edit's file_hash names the version the model read. The disk
// path checked it; the staging path — every dry run, which is the default —
// ignored it, and edited whatever the file had become since the read.
func TestEdit_StagingChecksTheFileHash(t *testing.T) {
	c, _ := workspaceWithFile(t, "a.go", "package a\n\nvar x = 1\n")
	read := fsutil.ComputeSHA256([]byte("package a\n\nvar x = 1\n"))

	// Another edit lands after the read.
	if _, err := c.Edit(context.Background(), FSEditRequest{Path: "a.go", Search: "var x = 1", Replace: "var x = 2", FileHash: read}); err != nil {
		t.Fatalf("an edit against the version read: %v", err)
	}
	_, err := c.Edit(context.Background(), FSEditRequest{Path: "a.go", Search: "package a", Replace: "package b", FileHash: read})
	if pe, ok := protocol.AsError(err); !ok || pe.Code != protocol.StaleContent {
		t.Fatalf("an edit against a stale read: %v, want StaleContent", err)
	}
	// No hash, no condition: the search block alone decides, as before.
	if _, err := c.Edit(context.Background(), FSEditRequest{Path: "a.go", Search: "package a", Replace: "package b"}); err != nil {
		t.Fatalf("an edit without a hash: %v", err)
	}
}
