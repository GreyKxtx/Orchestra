package packs

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSource_Local(t *testing.T) {
	dir := t.TempDir()
	src, err := ParseSource(dir)
	if err != nil {
		t.Fatal(err)
	}
	if src.Kind != SourceLocal {
		t.Errorf("kind: %q", src.Kind)
	}
	if src.Path == "" {
		t.Error("path empty")
	}
}

func TestParseSource_FileURL(t *testing.T) {
	src, err := ParseSource("file:///tmp/anything")
	if err != nil {
		t.Fatal(err)
	}
	if src.Kind != SourceLocal {
		t.Errorf("kind: %q", src.Kind)
	}
}

func TestParseSource_GitURL(t *testing.T) {
	cases := []string{
		"git@github.com:foo/bar.git",
		"https://github.com/foo/bar.git",
		"git+https://github.com/foo/bar",
	}
	for _, s := range cases {
		got, err := ParseSource(s)
		if err != nil {
			t.Errorf("%s: %v", s, err)
			continue
		}
		if got.Kind != SourceGit {
			t.Errorf("%s: kind %q (want git)", s, got.Kind)
		}
	}
}

func TestParseSource_HTTP(t *testing.T) {
	src, err := ParseSource("https://example.com/pack.zip")
	if err != nil {
		t.Fatal(err)
	}
	if src.Kind != SourceHTTP {
		t.Errorf("kind: %q", src.Kind)
	}
}

func TestParseSource_InvalidPath(t *testing.T) {
	_, err := ParseSource("/totally/does/not/exist/xyz")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseSource_Empty(t *testing.T) {
	_, err := ParseSource("  ")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSource_IDIsDeterministic(t *testing.T) {
	a, _ := ParseSource("https://github.com/foo/bar.git")
	b, _ := ParseSource("https://github.com/foo/bar.git")
	if a.ID() != b.ID() {
		t.Errorf("non-deterministic: %s vs %s", a.ID(), b.ID())
	}
	c, _ := ParseSource("https://github.com/foo/baz.git")
	if a.ID() == c.ID() {
		t.Errorf("collision: %s == %s", a.ID(), c.ID())
	}
}

func TestFetch_Local(t *testing.T) {
	srcDir := t.TempDir()
	os.WriteFile(filepath.Join(srcDir, "a.md"), []byte("---\nname: a\n---\n"), 0o644)
	os.MkdirAll(filepath.Join(srcDir, "sub"), 0o755)
	os.WriteFile(filepath.Join(srcDir, "sub", "b.md"), []byte("---\nname: b\n---\n"), 0o644)

	src, _ := ParseSource(srcDir)
	dest := filepath.Join(t.TempDir(), "pack")
	if _, err := Fetch(context.Background(), src, dest, FetchOptions{}); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	for _, p := range []string{"a.md", filepath.Join("sub", "b.md")} {
		if _, err := os.Stat(filepath.Join(dest, p)); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
}

func TestFetch_DestExistsRejected(t *testing.T) {
	srcDir := t.TempDir()
	dest := t.TempDir() // already exists
	src, _ := ParseSource(srcDir)
	_, err := Fetch(context.Background(), src, dest, FetchOptions{})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected already-exists error, got %v", err)
	}
}

func TestSafeExtractPath_RejectsTraversal(t *testing.T) {
	dest := filepath.Clean("/tmp/dest")
	cases := []string{
		"../escape.txt",
		"sub/../../escape.txt",
		"/absolute/path",
	}
	for _, c := range cases {
		if _, err := safeExtractPath(dest, c); err == nil {
			t.Errorf("%s: expected traversal error", c)
		}
	}
}

func TestExtractZip_HappyPath(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "p.zip")
	f, _ := os.Create(zipPath)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("foo.md")
	w.Write([]byte("hello"))
	zw.Close()
	f.Close()

	dest := filepath.Join(t.TempDir(), "out")
	os.MkdirAll(dest, 0o755)
	if err := extractZip(zipPath, dest); err != nil {
		t.Fatalf("extractZip: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(dest, "foo.md"))
	if string(b) != "hello" {
		t.Errorf("content: %q", string(b))
	}
}
