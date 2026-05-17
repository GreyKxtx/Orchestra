package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillsList_PrintsDiscoveredSkills(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".orchestra", "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "refactor.md"), []byte(
		"---\nname: refactor\ndescription: Refactor code.\n---\nbody\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "review.md"), []byte(
		"---\nname: review\ndescription: Code review pass.\n---\nbody\n"), 0o644)

	var out bytes.Buffer
	if err := RunSkillsList(root, &out); err != nil {
		t.Fatalf("RunSkillsList: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "refactor") || !strings.Contains(s, "Refactor code.") {
		t.Errorf("missing refactor row in output:\n%s", s)
	}
	if !strings.Contains(s, "review") || !strings.Contains(s, "Code review pass.") {
		t.Errorf("missing review row in output:\n%s", s)
	}
}

func TestSkillsList_EmptyDirIsNotError(t *testing.T) {
	var out bytes.Buffer
	if err := RunSkillsList(t.TempDir(), &out); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestSkillsShow_PrintsBody(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".orchestra", "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "x.md"), []byte(
		"---\nname: x\ndescription: D\ntools: [read, edit]\n---\nHello body.\n"), 0o644)

	var out bytes.Buffer
	if err := RunSkillsShow(root, "x", &out); err != nil {
		t.Fatalf("RunSkillsShow: %v", err)
	}
	s := out.String()
	for _, want := range []string{"x", "D", "read", "edit", "Hello body."} {
		if !strings.Contains(s, want) {
			t.Errorf("output missing %q:\n%s", want, s)
		}
	}
}

func TestSkillsShow_UnknownIsError(t *testing.T) {
	err := RunSkillsShow(t.TempDir(), "nope", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("expected unknown-skill error, got %v", err)
	}
}
