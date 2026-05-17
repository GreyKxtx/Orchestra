package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse_HappyPath(t *testing.T) {
	src := `---
name: refactor-go
description: Refactor Go code with conservative edits.
tools: [read, edit, write, grep, symbols]
model: qwen3.6-27b
---
You are a careful Go refactoring assistant. Use small, focused edits.
$ARGUMENTS
`
	s, err := Parse("refactor-go.md", strings.NewReader(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if s.Name != "refactor-go" {
		t.Errorf("name: got %q want refactor-go", s.Name)
	}
	if s.Description == "" {
		t.Error("description empty")
	}
	if len(s.Tools) != 5 || s.Tools[0] != "read" {
		t.Errorf("tools: got %v", s.Tools)
	}
	if s.Model != "qwen3.6-27b" {
		t.Errorf("model: got %q", s.Model)
	}
	if !strings.Contains(s.Body, "$ARGUMENTS") {
		t.Errorf("body missing args marker: %q", s.Body)
	}
}

func TestParse_MissingFrontmatter(t *testing.T) {
	_, err := Parse("x.md", strings.NewReader("just body\n"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "frontmatter open") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParse_UnterminatedFrontmatter(t *testing.T) {
	_, err := Parse("x.md", strings.NewReader("---\nname: x\n"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unterminated") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParse_InvalidYAML(t *testing.T) {
	_, err := Parse("x.md", strings.NewReader("---\nname: [bad\n---\nbody"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDiscover_ScansSkillsDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".orchestra", "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeSkill("a.md", "---\nname: a\ndescription: A\n---\nbody A\n")
	writeSkill("b.md", "---\nname: b\ndescription: B\n---\nbody B\n")
	writeSkill("not-a-skill.txt", "ignored")

	skills, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("got %d skills, want 2: %+v", len(skills), skills)
	}
	if skills[0].Name != "a" || skills[1].Name != "b" {
		t.Errorf("sorted by name failed: %q %q", skills[0].Name, skills[1].Name)
	}
}

func TestDiscover_MissingDirReturnsEmpty(t *testing.T) {
	root := t.TempDir()
	skills, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("expected empty, got %d", len(skills))
	}
}

func TestDiscover_DuplicateNameIsError(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".orchestra", "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("---\nname: dup\ndescription: x\n---\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("---\nname: dup\ndescription: y\n---\n"), 0o644)
	_, err := Discover(root)
	if err == nil {
		t.Fatal("expected duplicate-name error")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDiscover_MissingNameIsError(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".orchestra", "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "x.md"), []byte("---\ndescription: x\n---\n"), 0o644)
	_, err := Discover(root)
	if err == nil {
		t.Fatal("expected missing-name error")
	}
}

func TestFind(t *testing.T) {
	a := &Skill{Name: "a"}
	b := &Skill{Name: "b"}
	if got := Find([]*Skill{a, b}, "b"); got != b {
		t.Errorf("Find(b) = %v", got)
	}
	if got := Find([]*Skill{a, b}, "z"); got != nil {
		t.Errorf("Find(z) = %v, want nil", got)
	}
}
