package attachments

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStageIntoWorkspace_AlreadyInside(t *testing.T) {
	root := t.TempDir()
	inner := filepath.Join(root, "pic.png")
	if err := os.WriteFile(inner, []byte{0x89, 0x50, 0x4E, 0x47}, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := StageIntoWorkspace(root, inner)
	if err != nil {
		t.Fatal(err)
	}
	if got == "" {
		t.Fatal("empty path")
	}
}

func TestStageIntoWorkspace_CopiesExternal(t *testing.T) {
	root := t.TempDir()
	extDir := t.TempDir()
	src := filepath.Join(extDir, "outside.png")
	if err := os.WriteFile(src, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := StageIntoWorkspace(root, src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("staged file missing: %v", err)
	}
}

func TestMIMEForPath_SVGAndPDF(t *testing.T) {
	if MIMEForPath("x.svg") != "image/svg+xml" {
		t.Fatal("svg mime")
	}
	if MIMEForPath("doc.pdf") != "application/pdf" {
		t.Fatal("pdf mime")
	}
	if IsImagePath("doc.pdf") {
		t.Fatal("pdf should not be image path")
	}
}
