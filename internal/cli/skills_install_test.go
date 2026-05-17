package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewPack_AcceptsAll(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("---\nname: a\ndescription: D\n---\nbody\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("---\nname: b\ndescription: D\n---\nbody\n"), 0o644)

	var out bytes.Buffer
	in := strings.NewReader("y\ny\n")
	if err := reviewPack(context.Background(), &out, in, dir, "test", false); err != nil {
		t.Fatalf("reviewPack: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.md")); err != nil {
		t.Errorf("a.md should remain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "b.md")); err != nil {
		t.Errorf("b.md should remain: %v", err)
	}
	if !strings.Contains(out.String(), "Installed 2 skill") {
		t.Errorf("output: %s", out.String())
	}
}

func TestReviewPack_RejectsOne(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("---\nname: a\ndescription: D\n---\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("---\nname: b\ndescription: D\n---\n"), 0o644)

	var out bytes.Buffer
	in := strings.NewReader("n\ny\n")
	if err := reviewPack(context.Background(), &out, in, dir, "t", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.md")); err == nil {
		t.Error("a.md should have been deleted")
	}
	if _, err := os.Stat(filepath.Join(dir, "b.md")); err != nil {
		t.Errorf("b.md should remain: %v", err)
	}
}

func TestReviewPack_AllRejected_RemovesPackDir(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("---\nname: a\ndescription: D\n---\n"), 0o644)

	var out bytes.Buffer
	in := strings.NewReader("n\n")
	if err := reviewPack(context.Background(), &out, in, dir, "t", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("pack dir should have been removed")
	}
}

func TestReviewPack_NoSkills_RemovesPackDir(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "README.txt"), []byte("not a skill"), 0o644)

	var out bytes.Buffer
	in := strings.NewReader("")
	if err := reviewPack(context.Background(), &out, in, dir, "t", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("pack dir with no skills should have been removed")
	}
}

func TestReviewPack_AutoYes(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("---\nname: a\ndescription: D\n---\n"), 0o644)

	var out bytes.Buffer
	in := strings.NewReader("") // no input — autoYes shouldn't read it
	if err := reviewPack(context.Background(), &out, in, dir, "t", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.md")); err != nil {
		t.Errorf("a.md should remain under --yes: %v", err)
	}
	if !strings.Contains(out.String(), "WARNING") {
		t.Errorf("expected warning, got: %s", out.String())
	}
}

func TestReviewPack_SkipsInvalidSkill(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "bad.md"), []byte("not frontmatter\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "good.md"), []byte("---\nname: good\ndescription: D\n---\n"), 0o644)

	var out bytes.Buffer
	in := strings.NewReader("y\n")
	if err := reviewPack(context.Background(), &out, in, dir, "t", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "bad.md")); err == nil {
		t.Error("invalid skill should have been deleted")
	}
}
