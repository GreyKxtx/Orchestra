package attachments_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/attachments"
)

func TestValidatePaths_OutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.png")
	if err := os.WriteFile(outside, []byte{1}, 0o644); err != nil {
		t.Fatal(err)
	}
	err := attachments.ValidatePaths(root, []attachments.MessageAttachment{{Path: outside, Kind: "image"}})
	if err == nil {
		t.Fatal("expected validation error for path outside workspace")
	}
}

func TestValidatePaths_InsideWorkspace(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "ok.png")
	if err := os.WriteFile(inside, []byte{1}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := attachments.ValidatePaths(root, []attachments.MessageAttachment{{Path: inside, Kind: "image"}}); err != nil {
		t.Fatal(err)
	}
}
