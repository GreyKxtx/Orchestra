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

func TestDiscoverFrom_MergesUserAndProject(t *testing.T) {
	userDir := t.TempDir()
	projDir := t.TempDir()
	os.WriteFile(filepath.Join(userDir, "a.md"), []byte(
		"---\nname: a\ndescription: user-a\n---\nuser body\n"), 0o644)
	os.WriteFile(filepath.Join(userDir, "b.md"), []byte(
		"---\nname: b\ndescription: user-b\n---\nuser body\n"), 0o644)
	os.WriteFile(filepath.Join(projDir, "a.md"), []byte(
		"---\nname: a\ndescription: project-a\n---\nproject body\n"), 0o644)
	os.WriteFile(filepath.Join(projDir, "c.md"), []byte(
		"---\nname: c\ndescription: project-c\n---\nproject body\n"), 0o644)

	all, err := DiscoverFrom(userDir, projDir)
	if err != nil {
		t.Fatalf("DiscoverFrom: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("got %d, want 3 (a,b,c): %+v", len(all), names(all))
	}
	got := names(all)
	want := []string{"a", "b", "c"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sort: got %v want %v", got, want)
		}
	}
	a := Find(all, "a")
	if a.Description != "project-a" {
		t.Errorf("project should override user: got %q", a.Description)
	}
}

func TestDiscoverFrom_EmptyDirsAreOK(t *testing.T) {
	all, err := DiscoverFrom("", "")
	if err != nil {
		t.Fatalf("DiscoverFrom(\"\", \"\"): %v", err)
	}
	if len(all) != 0 {
		t.Errorf("expected empty, got %v", names(all))
	}
}

func TestDiscoverFrom_OnlyUser(t *testing.T) {
	userDir := t.TempDir()
	os.WriteFile(filepath.Join(userDir, "x.md"), []byte(
		"---\nname: x\ndescription: D\n---\n"), 0o644)
	all, err := DiscoverFrom(userDir, "")
	if err != nil {
		t.Fatalf("DiscoverFrom: %v", err)
	}
	if len(all) != 1 || all[0].Name != "x" {
		t.Errorf("got %v", names(all))
	}
}

func names(ss []*Skill) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.Name
	}
	return out
}

func TestDiscoverFromAll_PrecedenceProjectOverUserOverPack(t *testing.T) {
	packsRoot := t.TempDir()
	pack := filepath.Join(packsRoot, "github_com_foo_bar")
	os.MkdirAll(pack, 0o755)
	os.WriteFile(filepath.Join(pack, "a.md"), []byte("---\nname: a\ndescription: pack-a\n---\nbody\n"), 0o644)

	userDir := t.TempDir()
	os.WriteFile(filepath.Join(userDir, "a.md"), []byte("---\nname: a\ndescription: user-a\n---\n"), 0o644)
	os.WriteFile(filepath.Join(userDir, "b.md"), []byte("---\nname: b\ndescription: user-b\n---\n"), 0o644)

	projDir := t.TempDir()
	os.WriteFile(filepath.Join(projDir, "a.md"), []byte("---\nname: a\ndescription: proj-a\n---\n"), 0o644)
	os.WriteFile(filepath.Join(projDir, "c.md"), []byte("---\nname: c\ndescription: proj-c\n---\n"), 0o644)

	all, err := DiscoverFromAll(packsRoot, userDir, projDir)
	if err != nil {
		t.Fatalf("DiscoverFromAll: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("got %d, want 3: %v", len(all), names(all))
	}
	a := Find(all, "a")
	if a.Description != "proj-a" {
		t.Errorf("project should win: got %q", a.Description)
	}
	if a.Origin != OriginProject {
		t.Errorf("origin: %q", a.Origin)
	}
	b := Find(all, "b")
	if b.Origin != OriginUser {
		t.Errorf("b origin: %q", b.Origin)
	}
}

func TestDiscoverFromAll_PackOnlySkill(t *testing.T) {
	packsRoot := t.TempDir()
	pack := filepath.Join(packsRoot, "mypack")
	os.MkdirAll(filepath.Join(pack, "nested"), 0o755)
	os.WriteFile(filepath.Join(pack, "nested", "deep.md"), []byte("---\nname: deep\ndescription: D\n---\n"), 0o644)

	all, err := DiscoverFromAll(packsRoot, "", "")
	if err != nil {
		t.Fatalf("DiscoverFromAll: %v", err)
	}
	if len(all) != 1 || all[0].Name != "deep" {
		t.Fatalf("got %v", names(all))
	}
	if all[0].Origin != "pack:mypack" {
		t.Errorf("origin: %q", all[0].Origin)
	}
}

func TestDiscoverFromAll_DuplicateInSamePack(t *testing.T) {
	packsRoot := t.TempDir()
	pack := filepath.Join(packsRoot, "p")
	os.MkdirAll(pack, 0o755)
	os.WriteFile(filepath.Join(pack, "a.md"), []byte("---\nname: dup\ndescription: x\n---\n"), 0o644)
	os.WriteFile(filepath.Join(pack, "b.md"), []byte("---\nname: dup\ndescription: y\n---\n"), 0o644)
	_, err := DiscoverFromAll(packsRoot, "", "")
	if err == nil {
		t.Fatal("expected duplicate error")
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
