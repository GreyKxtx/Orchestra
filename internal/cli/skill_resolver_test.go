package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveSkillAgent_LoadsAndMaps(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".orchestra", "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "minimal.md"), []byte(
		"---\nname: minimal\ndescription: D\ntools: [read]\nmodel: qwen3.6-27b\n---\nYou are minimal.\n"), 0o644)

	def, err := resolveSkillAgent(root, "minimal", "")
	if err != nil {
		t.Fatalf("resolveSkillAgent: %v", err)
	}
	if def.Name != "minimal" {
		t.Errorf("Name: %q", def.Name)
	}
	if !strings.Contains(def.SystemPrompt, "minimal") {
		t.Errorf("SystemPrompt: %q", def.SystemPrompt)
	}
	if len(def.Tools) != 1 || def.Tools[0] != "read" {
		t.Errorf("Tools: %v", def.Tools)
	}
	if def.Model != "qwen3.6-27b" {
		t.Errorf("Model: %q", def.Model)
	}
}

func TestResolveSkillAgent_UnknownSkill(t *testing.T) {
	_, err := resolveSkillAgent(t.TempDir(), "nope", "")
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("expected unknown-skill error, got %v", err)
	}
}

func TestResolveSkillAgent_InvalidTool(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".orchestra", "skills")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "bad.md"), []byte(
		"---\nname: bad\ndescription: D\ntools: [definitely-not-a-real-tool]\n---\n"), 0o644)
	_, err := resolveSkillAgent(root, "bad", "")
	if err == nil || !strings.Contains(err.Error(), "definitely-not-a-real-tool") {
		t.Fatalf("expected invalid-tool error, got %v", err)
	}
}

func TestResolveSkillAgent_ArgumentsSubstitution(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".orchestra", "skills")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "tmpl.md"), []byte(
		"---\nname: tmpl\ndescription: D\n---\nRefactor: $ARGUMENTS\n"), 0o644)

	def, err := resolveSkillAgent(root, "tmpl", "internal/foo.go")
	if err != nil {
		t.Fatalf("resolveSkillAgent: %v", err)
	}
	if !strings.Contains(def.SystemPrompt, "Refactor: internal/foo.go") {
		t.Errorf("substitution failed: %q", def.SystemPrompt)
	}
	if strings.Contains(def.SystemPrompt, "$ARGUMENTS") {
		t.Errorf("marker still present: %q", def.SystemPrompt)
	}
}

func TestResolveSkillAgent_NoMarkerNoSubstitution(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".orchestra", "skills")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "plain.md"), []byte(
		"---\nname: plain\ndescription: D\n---\nPlain body without marker.\n"), 0o644)
	def, err := resolveSkillAgent(root, "plain", "ignored args")
	if err != nil {
		t.Fatalf("resolveSkillAgent: %v", err)
	}
	if def.SystemPrompt != "Plain body without marker.\n" {
		t.Errorf("body unexpectedly changed: %q", def.SystemPrompt)
	}
}

func TestResolveSkillAgent_BuiltinNameCollides(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".orchestra", "skills")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "plan.md"), []byte(
		"---\nname: plan\ndescription: D\n---\n"), 0o644)
	_, err := resolveSkillAgent(root, "plan", "")
	if err == nil || !strings.Contains(err.Error(), "built-in") {
		t.Fatalf("expected built-in collision error, got %v", err)
	}
}
