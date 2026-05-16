package search

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// --- parseRipgrepJSON unit tests (no rg binary needed) ---

func TestParseRipgrepJSON_SingleMatch(t *testing.T) {
	input := `{"type":"begin","data":{"path":{"text":"main.go"}}}
{"type":"match","data":{"path":{"text":"main.go"},"line_number":5,"lines":{"text":"func main() {\n"},"submatches":[]}}
{"type":"end","data":{"path":{"text":"main.go"}}}
{"type":"summary","data":{"elapsed_total":{"secs":0},"stats":{"matches":1,"matched_lines":1,"searches":1,"searches_with_match":1,"bytes_searched":0,"bytes_printed":0}}}
`
	matches := parseRipgrepJSON("/project", []byte(input), 0)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	m := matches[0]
	if m.Line != 5 {
		t.Errorf("expected line 5, got %d", m.Line)
	}
	if m.LineText != "func main() {" {
		t.Errorf("unexpected LineText: %q", m.LineText)
	}
	if filepath.Base(m.FilePath) != "main.go" {
		t.Errorf("unexpected FilePath: %q", m.FilePath)
	}
	if len(m.ContextBefore) != 0 || len(m.ContextAfter) != 0 {
		t.Errorf("expected no context lines, got before=%v after=%v", m.ContextBefore, m.ContextAfter)
	}
}

func TestParseRipgrepJSON_WithContext(t *testing.T) {
	input := `{"type":"begin","data":{"path":{"text":"foo.go"}}}
{"type":"context","data":{"path":{"text":"foo.go"},"line_number":2,"lines":{"text":"// comment\n"}}}
{"type":"match","data":{"path":{"text":"foo.go"},"line_number":3,"lines":{"text":"func init() {\n"},"submatches":[]}}
{"type":"context","data":{"path":{"text":"foo.go"},"line_number":4,"lines":{"text":"\tsetup()\n"}}}
{"type":"end","data":{"path":{"text":"foo.go"}}}
`
	matches := parseRipgrepJSON("/project", []byte(input), 1)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	m := matches[0]
	if len(m.ContextBefore) != 1 || m.ContextBefore[0] != "// comment" {
		t.Errorf("unexpected ContextBefore: %v", m.ContextBefore)
	}
	if len(m.ContextAfter) != 1 || m.ContextAfter[0] != "\tsetup()" {
		t.Errorf("unexpected ContextAfter: %v", m.ContextAfter)
	}
}

func TestParseRipgrepJSON_MultiFile(t *testing.T) {
	input := `{"type":"begin","data":{"path":{"text":"a.go"}}}
{"type":"match","data":{"path":{"text":"a.go"},"line_number":1,"lines":{"text":"package main\n"},"submatches":[]}}
{"type":"end","data":{"path":{"text":"a.go"}}}
{"type":"begin","data":{"path":{"text":"b.go"}}}
{"type":"match","data":{"path":{"text":"b.go"},"line_number":2,"lines":{"text":"package main\n"},"submatches":[]}}
{"type":"end","data":{"path":{"text":"b.go"}}}
`
	matches := parseRipgrepJSON("/project", []byte(input), 0)
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
	if filepath.Base(matches[0].FilePath) != "a.go" {
		t.Errorf("expected a.go, got %q", matches[0].FilePath)
	}
	if filepath.Base(matches[1].FilePath) != "b.go" {
		t.Errorf("expected b.go, got %q", matches[1].FilePath)
	}
}

func TestParseRipgrepJSON_Empty(t *testing.T) {
	input := `{"type":"summary","data":{"elapsed_total":{"secs":0},"stats":{"matches":0}}}
`
	matches := parseRipgrepJSON("/project", []byte(input), 0)
	if len(matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(matches))
	}
}

func TestParseRipgrepJSON_ContextDoesNotCrossFiles(t *testing.T) {
	input := `{"type":"begin","data":{"path":{"text":"a.go"}}}
{"type":"match","data":{"path":{"text":"a.go"},"line_number":10,"lines":{"text":"match in a\n"},"submatches":[]}}
{"type":"context","data":{"path":{"text":"a.go"},"line_number":11,"lines":{"text":"after a\n"}}}
{"type":"end","data":{"path":{"text":"a.go"}}}
{"type":"begin","data":{"path":{"text":"b.go"}}}
{"type":"match","data":{"path":{"text":"b.go"},"line_number":1,"lines":{"text":"match in b\n"},"submatches":[]}}
{"type":"end","data":{"path":{"text":"b.go"}}}
`
	matches := parseRipgrepJSON("/project", []byte(input), 3)
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
	if len(matches[1].ContextBefore) != 0 {
		t.Errorf("b.go match should have no before-context, got %v", matches[1].ContextBefore)
	}
}

// --- SearchWithRipgrep integration tests (skipped if rg not in PATH) ---

func TestSearchWithRipgrep_EmptyQuery(t *testing.T) {
	dir := t.TempDir()
	opts := DefaultOptions()
	_, err := SearchWithRipgrep(dir, "", nil, opts, nil)
	if err == nil {
		t.Error("expected error for empty query, got nil")
	}
}

func TestSearchWithRipgrep_Basic(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not in PATH")
	}
	dir := t.TempDir()
	writeSearchFile(t, dir, "hello.go", "package main\n\nfunc hello() {}\n")
	writeSearchFile(t, dir, "world.go", "package main\n\nfunc world() {}\n")

	opts := DefaultOptions()
	opts.ContextLines = 0
	matches, err := SearchWithRipgrep(dir, "hello", nil, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatal("expected at least one match")
	}
	found := false
	for _, m := range matches {
		if filepath.Base(m.FilePath) == "hello.go" {
			found = true
		}
	}
	if !found {
		t.Error("expected match in hello.go")
	}
}

func TestSearchWithRipgrep_CaseInsensitive(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not in PATH")
	}
	dir := t.TempDir()
	writeSearchFile(t, dir, "f.go", "package main\nfunc Hello() {}\nfunc hello() {}\n")

	opts := DefaultOptions()
	opts.CaseInsensitive = true
	opts.ContextLines = 0
	matches, err := SearchWithRipgrep(dir, "HELLO", nil, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Errorf("expected 2 case-insensitive matches, got %d", len(matches))
	}
}

func TestSearchWithRipgrep_NoMatches(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not in PATH")
	}
	dir := t.TempDir()
	writeSearchFile(t, dir, "f.go", "package main\n")

	opts := DefaultOptions()
	matches, err := SearchWithRipgrep(dir, "NONEXISTENT_STRING_XYZ", nil, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(matches))
	}
}

func TestSearchWithRipgrep_ExcludeDirs(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not in PATH")
	}
	dir := t.TempDir()
	writeSearchFile(t, dir, "root.go", "func target() {}")
	writeSearchFile(t, dir, "vendor/dep.go", "func target() {}")

	opts := DefaultOptions()
	opts.ContextLines = 0
	matches, err := SearchWithRipgrep(dir, "target", []string{"vendor"}, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range matches {
		if filepath.Base(filepath.Dir(m.FilePath)) == "vendor" {
			t.Errorf("vendor dir should be excluded, got match in %s", m.FilePath)
		}
	}
	if len(matches) == 0 {
		t.Error("expected at least one match in root.go")
	}
}

func writeSearchFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
