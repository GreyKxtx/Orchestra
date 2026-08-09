package attachments_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/attachments"
)

func TestMergeQueryWithFileRefs(t *testing.T) {
	q := attachments.MergeQueryWithFileRefs("hello", []attachments.MessageAttachment{
		{Path: "/tmp/a.go", Kind: "file"},
		{Path: "/tmp/b.png", Kind: "image"},
	})
	if q != "hello\n\n@/tmp/a.go" {
		t.Fatalf("got %q", q)
	}
}

func TestLoadImageParts(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.png")
	if err := os.WriteFile(p, []byte{0x89, 0x50, 0x4e, 0x47}, 0o644); err != nil {
		t.Fatal(err)
	}
	parts, err := attachments.LoadImageParts([]attachments.MessageAttachment{{Path: p, Kind: "image"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
}
