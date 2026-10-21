package config

import (
	"os"
	"path/filepath"
	"testing"
)

// A project whose model was never picked loads with none, and the
// placeholder `orchestra init` used to write is not a pick when nothing on
// this machine serves it.
func TestAModelNeverPickedLoadsAsNone(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	write := func(t *testing.T, yml string) *ProjectConfig {
		t.Helper()
		root := t.TempDir()
		path := filepath.Join(root, ".orchestra.yml")
		if err := os.WriteFile(path, []byte(yml), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		return cfg
	}

	none := write(t, "project_root: .\n")
	if none.LLM.Model != "" || none.LLM.APIBase != "" {
		t.Fatalf("a project with no llm: got %q at %q", none.LLM.Model, none.LLM.APIBase)
	}

	left := write(t, "project_root: .\nllm:\n  provider: anthropic\n  api_base: https://api.anthropic.com\n  model: qwen2.5-coder-7b\nproviders:\n  anthropic:\n    provider: anthropic\n    api_base: https://api.anthropic.com\n    model: qwen2.5-coder-7b\n")
	if left.LLM.Model != "" {
		t.Fatalf("Anthropic kept the init placeholder as its model: %q", left.LLM.Model)
	}
	if m := left.Providers["anthropic"].Model; m != "" {
		t.Fatalf("providers.anthropic kept the init placeholder: %q", m)
	}

	local := write(t, "project_root: .\nllm:\n  api_base: http://localhost:1234/v1\n  model: qwen2.5-coder-7b\n")
	if local.LLM.Model != InitPlaceholderModel {
		t.Fatalf("LM Studio serving the model lost it: %q", local.LLM.Model)
	}

	root := t.TempDir()
	path := filepath.Join(root, ".orchestra.yml")
	if err := os.WriteFile(path, []byte("project_root: .\nllm:\n  model: claude-opus-5-5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("a model with no endpoint loaded")
	}
}
