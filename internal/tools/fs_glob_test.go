package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newGlobRunner(t *testing.T) (*Runner, string) {
	t.Helper()
	root := t.TempDir()
	r, err := NewRunner(root, RunnerOptions{})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r, root
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile %s: %v", rel, err)
	}
}

func globPaths(resp *FSGlobResponse) []string {
	out := make([]string, 0, len(resp.Files))
	for _, f := range resp.Files {
		out = append(out, f.Path)
	}
	return out
}

func containsPath(paths []string, want string) bool {
	for _, p := range paths {
		if p == want {
			return true
		}
	}
	return false
}

func TestFSGlob_BasicWildcard(t *testing.T) {
	r, root := newGlobRunner(t)
	writeFile(t, root, "main.go", "package main")
	writeFile(t, root, "util.go", "package main")
	writeFile(t, root, "README.md", "# readme")

	resp, err := r.FSGlob(context.Background(), FSGlobRequest{Pattern: "*.go"})
	if err != nil {
		t.Fatalf("FSGlob: %v", err)
	}
	paths := globPaths(resp)
	if !containsPath(paths, "main.go") || !containsPath(paths, "util.go") {
		t.Errorf("expected main.go and util.go, got %v", paths)
	}
	if containsPath(paths, "README.md") {
		t.Errorf("README.md should not match *.go, got %v", paths)
	}
}

func TestFSGlob_DoubleStarRecursive(t *testing.T) {
	r, root := newGlobRunner(t)
	writeFile(t, root, "a.go", "")
	writeFile(t, root, "sub/b.go", "")
	writeFile(t, root, "sub/deep/c.go", "")
	writeFile(t, root, "sub/deep/d.txt", "")

	resp, err := r.FSGlob(context.Background(), FSGlobRequest{Pattern: "**/*.go"})
	if err != nil {
		t.Fatalf("FSGlob: %v", err)
	}
	paths := globPaths(resp)
	for _, want := range []string{"a.go", "sub/b.go", "sub/deep/c.go"} {
		if !containsPath(paths, want) {
			t.Errorf("expected %s in results, got %v", want, paths)
		}
	}
	if containsPath(paths, "sub/deep/d.txt") {
		t.Errorf("d.txt should not match **/*.go")
	}
}

func TestFSGlob_PathPrefix(t *testing.T) {
	r, root := newGlobRunner(t)
	writeFile(t, root, "internal/a.go", "")
	writeFile(t, root, "internal/b.go", "")
	writeFile(t, root, "cmd/main.go", "")

	resp, err := r.FSGlob(context.Background(), FSGlobRequest{Pattern: "internal/*.go"})
	if err != nil {
		t.Fatalf("FSGlob: %v", err)
	}
	paths := globPaths(resp)
	if !containsPath(paths, "internal/a.go") || !containsPath(paths, "internal/b.go") {
		t.Errorf("expected internal/*.go files, got %v", paths)
	}
	if containsPath(paths, "cmd/main.go") {
		t.Errorf("cmd/main.go should not match internal/*.go")
	}
}

func TestFSGlob_DoubleStarMatchesZeroSegments(t *testing.T) {
	r, root := newGlobRunner(t)
	writeFile(t, root, "internal/tools/x.go", "")

	// "internal/**/*.go" should also match "internal/tools/x.go"
	resp, err := r.FSGlob(context.Background(), FSGlobRequest{Pattern: "internal/**/*.go"})
	if err != nil {
		t.Fatalf("FSGlob: %v", err)
	}
	if !containsPath(globPaths(resp), "internal/tools/x.go") {
		t.Errorf("expected internal/tools/x.go, got %v", globPaths(resp))
	}
}

func TestFSGlob_ExcludeDirs(t *testing.T) {
	r, root := newGlobRunner(t)
	writeFile(t, root, "a.go", "")
	writeFile(t, root, "vendor/v.go", "")

	resp, err := r.FSGlob(context.Background(), FSGlobRequest{
		Pattern:     "**/*.go",
		ExcludeDirs: []string{"vendor"},
	})
	if err != nil {
		t.Fatalf("FSGlob: %v", err)
	}
	if containsPath(globPaths(resp), "vendor/v.go") {
		t.Errorf("vendor/v.go should be excluded")
	}
	if !containsPath(globPaths(resp), "a.go") {
		t.Errorf("a.go should be included")
	}
}

func TestFSGlob_Limit(t *testing.T) {
	r, root := newGlobRunner(t)
	for i := 0; i < 5; i++ {
		writeFile(t, root, strings.Repeat("x", i+1)+".go", "")
	}

	resp, err := r.FSGlob(context.Background(), FSGlobRequest{Pattern: "*.go", Limit: 3})
	if err != nil {
		t.Fatalf("FSGlob: %v", err)
	}
	if len(resp.Files) > 3 {
		t.Errorf("expected at most 3 files, got %d", len(resp.Files))
	}
}

func TestFSGlob_EmptyPattern(t *testing.T) {
	r, _ := newGlobRunner(t)
	_, err := r.FSGlob(context.Background(), FSGlobRequest{Pattern: ""})
	if err == nil {
		t.Fatal("expected error for empty pattern")
	}
}

func TestFSGlob_AbsolutePattern(t *testing.T) {
	r, _ := newGlobRunner(t)
	_, err := r.FSGlob(context.Background(), FSGlobRequest{Pattern: "/etc/passwd"})
	if err == nil {
		t.Fatal("expected error for absolute pattern")
	}
}

func TestFSGlob_DotDotSegment(t *testing.T) {
	r, _ := newGlobRunner(t)
	_, err := r.FSGlob(context.Background(), FSGlobRequest{Pattern: "../other/*.go"})
	if err == nil {
		t.Fatal("expected error for .. in pattern")
	}
}

func TestFSGlob_LegitDoubleDotInFilename(t *testing.T) {
	// "file..bak" should NOT be rejected — ".." as a path segment is forbidden,
	// but ".." embedded in a filename segment is fine.
	r, root := newGlobRunner(t)
	writeFile(t, root, "file..bak", "")

	_, err := r.FSGlob(context.Background(), FSGlobRequest{Pattern: "file..bak"})
	if err != nil {
		t.Fatalf("file..bak pattern should be valid, got error: %v", err)
	}
}

func TestFSGlob_IncludeHash(t *testing.T) {
	r, root := newGlobRunner(t)
	writeFile(t, root, "a.go", "package main\n")

	resp, err := r.FSGlob(context.Background(), FSGlobRequest{Pattern: "*.go", IncludeHash: true})
	if err != nil {
		t.Fatalf("FSGlob: %v", err)
	}
	if len(resp.Files) == 0 {
		t.Fatal("expected at least one file")
	}
	if resp.Files[0].FileHash == "" {
		t.Error("expected non-empty file_hash when include_hash=true")
	}
}

func TestFSGlob_PatternReturnedInResponse(t *testing.T) {
	r, root := newGlobRunner(t)
	writeFile(t, root, "a.go", "")

	resp, err := r.FSGlob(context.Background(), FSGlobRequest{Pattern: "*.go"})
	if err != nil {
		t.Fatalf("FSGlob: %v", err)
	}
	if resp.Pattern != "*.go" {
		t.Errorf("expected pattern=*.go, got %q", resp.Pattern)
	}
}

// matchGlobPath unit tests

func TestMatchGlobPath_Star(t *testing.T) {
	if !matchGlobPath("*.go", "main.go") {
		t.Error("*.go should match main.go")
	}
	if matchGlobPath("*.go", "sub/main.go") {
		t.Error("*.go should not match sub/main.go")
	}
}

func TestMatchGlobPath_DoubleStar(t *testing.T) {
	if !matchGlobPath("**/*.go", "main.go") {
		t.Error("**/*.go should match main.go (zero segments)")
	}
	if !matchGlobPath("**/*.go", "a/b/c.go") {
		t.Error("**/*.go should match a/b/c.go")
	}
	if matchGlobPath("**/*.go", "a/b/c.txt") {
		t.Error("**/*.go should not match a/b/c.txt")
	}
}

func TestMatchGlobPath_DoubleStarAlone(t *testing.T) {
	if !matchGlobPath("**", "anything") {
		t.Error("** should match anything")
	}
	if !matchGlobPath("**", "a/b/c") {
		t.Error("** should match a/b/c")
	}
}
