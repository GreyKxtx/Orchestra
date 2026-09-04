package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolateHome points os.UserHomeDir at a temp dir on both Windows and Unix so
// a developer's real ~/.orchestra/config.yml cannot influence assertions.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func writeGlobalConfig(t *testing.T, home, body string) {
	t.Helper()
	dir := filepath.Join(home, ".orchestra")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, GlobalConfigName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeProjectConfig(t *testing.T, root, body string) string {
	t.Helper()
	path := filepath.Join(root, ".orchestra.yml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad_InheritsFromGlobalConfig(t *testing.T) {
	home := isolateHome(t)
	root := t.TempDir()
	writeGlobalConfig(t, home, "llm:\n  api_key: user-wide-key\n  model: global-model\n  api_base: http://global/v1\n")
	path := writeProjectConfig(t, root, "project_root: "+filepath.ToSlash(root)+"\nllm:\n  model: project-model\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	// The point of a user-level config: stop retyping credentials and endpoints
	// in every repo. Anything the project states still wins.
	if cfg.LLM.APIKey != "user-wide-key" {
		t.Errorf("global api_key must reach the project, got %q", cfg.LLM.APIKey)
	}
	if cfg.LLM.APIBase != "http://global/v1" {
		t.Errorf("global api_base must reach the project, got %q", cfg.LLM.APIBase)
	}
	if cfg.LLM.Model != "project-model" {
		t.Errorf("project must win over global, got %q", cfg.LLM.Model)
	}
}

func TestLoad_GlobalConfigCannotSetProjectRoot(t *testing.T) {
	home := isolateHome(t)
	root := t.TempDir()
	other := t.TempDir()
	writeGlobalConfig(t, home, "project_root: "+filepath.ToSlash(other)+"\nllm:\n  model: m\n  api_base: http://global/v1\n")
	path := writeProjectConfig(t, root, "project_root: "+filepath.ToSlash(root)+"\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	// A user-level project_root would point every repo at one directory — the
	// agent would read and write entirely the wrong tree.
	if filepath.Clean(cfg.ProjectRoot) != filepath.Clean(root) {
		t.Fatalf("project_root = %q, want the project's own root %q", cfg.ProjectRoot, root)
	}
}

func TestLoad_LocalOverlayBeatsGlobal(t *testing.T) {
	home := isolateHome(t)
	root := t.TempDir()
	writeGlobalConfig(t, home, "llm:\n  api_key: global-key\n  model: m\n  api_base: http://global/v1\n")
	path := writeProjectConfig(t, root, "project_root: "+filepath.ToSlash(root)+"\n")
	if err := os.WriteFile(filepath.Join(root, LocalOverlayName),
		[]byte("llm:\n  api_key: machine-key\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	// Precedence is global < project < local: the machine-specific overlay is
	// the most specific statement about this checkout.
	if cfg.LLM.APIKey != "machine-key" {
		t.Fatalf("local overlay must win, got %q", cfg.LLM.APIKey)
	}
}

func TestLoad_WorksWithoutAGlobalConfig(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	path := writeProjectConfig(t, root, "project_root: "+filepath.ToSlash(root)+"\nllm:\n  model: only-project\n  api_base: http://project/v1\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.Model != "only-project" {
		t.Fatalf("model = %q", cfg.LLM.Model)
	}
}

func TestSave_DoesNotCopyGlobalSecretsIntoTheProjectFile(t *testing.T) {
	home := isolateHome(t)
	root := t.TempDir()
	writeGlobalConfig(t, home, "llm:\n  api_key: user-wide-secret\n  api_base: http://global/v1\n")
	path := writeProjectConfig(t, root, "project_root: "+filepath.ToSlash(root)+"\nllm:\n  model: m\n")
	// Precondition: api_base is required, and only the global file supplies it —
	// so a successful Load already proves inheritance happened.

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.APIKey != "user-wide-secret" {
		t.Fatalf("precondition: expected the global key in memory, got %q", cfg.LLM.APIKey)
	}
	// A settings round-trip through the TUI or the extension does exactly this.
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}

	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// .orchestra.yml is committed. Writing back a value the user put in their
	// home directory would publish it to the repository.
	if strings.Contains(string(onDisk), "user-wide-secret") {
		t.Fatalf("global secret leaked into the shared project config:\n%s", onDisk)
	}
	if strings.Contains(string(onDisk), "http://global/v1") {
		t.Fatalf("global api_base leaked into the shared project config:\n%s", onDisk)
	}
}
