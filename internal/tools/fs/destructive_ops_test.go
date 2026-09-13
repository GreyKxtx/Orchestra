package fs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fs.delete and fs.rename did nothing under an agent run and said they had.
//
// The agent always stages: core calls SetDryRun(true) for every run and the
// per-tool commit puts write/edit on disk afterwards. delete and rename had no
// such commit — and their own dry-run branch returned the same success
// response as a real delete, with no record kept anywhere. So the model was
// told the file was gone, ls still showed it, and the turn died repeating the
// call. From the first run of the first eval task that ever graded a delete:
//
//	tool_call   fs.delete           -> output_bytes 20   (success)
//	tool_call   ls                  -> 263 bytes, the file still listed
//	tool_call   fs.delete           -> denied: duplicate call
//
// The file survived the whole run.

func workspaceWithFile(t *testing.T, name, body string) (*Client, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	overlay := NewOverlay(root, OverlayOptions{DryRun: true})
	return NewClient(root, nil, overlay), root
}

func TestDelete_AnApplyingRunRemovesTheFile(t *testing.T) {
	c, root := workspaceWithFile(t, "legacy.go", "package main\n")
	c.Overlay.SetCommitsToDisk(true)

	if _, err := c.Delete(context.Background(), FSDeleteRequest{Path: "legacy.go"}); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "legacy.go")); !os.IsNotExist(err) {
		t.Fatal("the file is still on disk: delete reported success and did nothing")
	}
}

func TestRename_AnApplyingRunMovesTheFile(t *testing.T) {
	c, root := workspaceWithFile(t, "util.go", "package main\n")
	c.Overlay.SetCommitsToDisk(true)

	if _, err := c.Rename(context.Background(), FSRenameRequest{Path: "util.go", NewPath: "geometry.go"}); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "util.go")); !os.IsNotExist(err) {
		t.Error("the old name is still there")
	}
	body, err := os.ReadFile(filepath.Join(root, "geometry.go"))
	if err != nil {
		t.Fatalf("the new name does not exist: %v", err)
	}
	if string(body) != "package main\n" {
		t.Errorf("the contents did not travel: %q", body)
	}
}

// A run that only previews must still not touch the disk — and must not claim
// it did. Saying "deleted" about a file that is still there is the defect;
// leaving it alone silently would be the same defect wearing the other mask.
func TestDelete_APreviewRunLeavesTheFileAndSaysSo(t *testing.T) {
	c, root := workspaceWithFile(t, "legacy.go", "package main\n")

	resp, err := c.Delete(context.Background(), FSDeleteRequest{Path: "legacy.go"})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, statErr := os.Stat(filepath.Join(root, "legacy.go")); statErr != nil {
		t.Fatal("a preview run must not remove anything")
	}
	if resp.Pending == "" {
		t.Fatal("a preview that answers exactly like a completed delete is how the " +
			"model learns to trust a deletion that never happened")
	}
	if !strings.Contains(resp.Pending, "legacy.go") {
		t.Errorf("the note must name the file it is about: %q", resp.Pending)
	}
}

func TestRename_APreviewRunLeavesTheFileAndSaysSo(t *testing.T) {
	c, root := workspaceWithFile(t, "util.go", "package main\n")

	resp, err := c.Rename(context.Background(), FSRenameRequest{Path: "util.go", NewPath: "geometry.go"})
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}

	if _, statErr := os.Stat(filepath.Join(root, "util.go")); statErr != nil {
		t.Fatal("a preview run must not move anything")
	}
	if resp.Pending == "" {
		t.Fatal("a preview must not answer exactly like a completed rename")
	}
}
