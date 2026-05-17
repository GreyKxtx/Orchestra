package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/llm"
)

func TestLoadImageParts_HappyPath(t *testing.T) {
	dir := t.TempDir()
	png := filepath.Join(dir, "x.png")
	jpg := filepath.Join(dir, "y.jpeg")
	os.WriteFile(png, []byte{0x89, 0x50, 0x4E, 0x47}, 0o644)
	os.WriteFile(jpg, []byte{0xFF, 0xD8, 0xFF, 0xE0}, 0o644)

	parts, err := loadImageParts([]string{png, jpg})
	if err != nil {
		t.Fatalf("loadImageParts: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("len: %d", len(parts))
	}
	if parts[0].Kind != llm.PartImage || parts[0].ImageMIME != "image/png" {
		t.Errorf("part[0]: %+v", parts[0])
	}
	if parts[1].ImageMIME != "image/jpeg" {
		t.Errorf("part[1] mime: %q", parts[1].ImageMIME)
	}
}

func TestLoadImageParts_UnsupportedExt(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "x.bmp")
	os.WriteFile(f, []byte{1, 2}, 0o644)
	_, err := loadImageParts([]string{f})
	if err == nil || !strings.Contains(err.Error(), "unsupported extension") {
		t.Fatalf("expected unsupported error, got %v", err)
	}
}

func TestLoadImageParts_MissingFile(t *testing.T) {
	_, err := loadImageParts([]string{"/no/such/file.png"})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadImageParts_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "x.png")
	os.WriteFile(f, nil, 0o644)
	_, err := loadImageParts([]string{f})
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected empty-file error, got %v", err)
	}
}

func TestLoadImageParts_EmptyInput(t *testing.T) {
	parts, err := loadImageParts(nil)
	if err != nil || parts != nil {
		t.Errorf("nil input: got parts=%v err=%v", parts, err)
	}
}
