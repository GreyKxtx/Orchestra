package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
)

func TestProviderNames_Empty(t *testing.T) {
	cfg := &config.ProjectConfig{}
	got := providerNames(cfg)
	if got != "(none configured)" {
		t.Errorf("providerNames(empty) = %q, want %q", got, "(none configured)")
	}
}

func TestProviderNames_Single(t *testing.T) {
	cfg := &config.ProjectConfig{
		Providers: map[string]config.LLMConfig{
			"anthropic": {Model: "claude-opus"},
		},
	}
	got := providerNames(cfg)
	if got != "anthropic" {
		t.Errorf("providerNames(single) = %q, want %q", got, "anthropic")
	}
}

func TestProviderNames_Multiple_Sorted(t *testing.T) {
	cfg := &config.ProjectConfig{
		Providers: map[string]config.LLMConfig{
			"openai":    {Model: "gpt-4"},
			"anthropic": {Model: "claude-opus"},
			"local":     {Model: "llama"},
		},
	}
	got := providerNames(cfg)
	want := "anthropic, local, openai"
	if got != want {
		t.Errorf("providerNames(multiple) = %q, want %q", got, want)
	}
}

// TestRunApply_ProviderFlagUnknown verifies that runApply returns an error
// when --provider references a name not present in the providers: section.
func TestRunApply_ProviderFlagUnknown(t *testing.T) {
	// Write a minimal .orchestra.yml into a temp dir.
	tmpDir := t.TempDir()
	cfgContent := `project_root: "` + filepath.ToSlash(tmpDir) + `"
llm:
  api_base: http://localhost:8000/v1
  model: test-model
  timeout_s: 30
context_limit_kb: 50
limits:
  context_kb: 50
exec:
  timeout_s: 30
  output_limit_kb: 100
providers:
  myprovider:
    api_base: http://other:8000/v1
    model: other-model
    timeout_s: 30
`
	cfgPath := filepath.Join(tmpDir, ".orchestra.yml")
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Change to tmpDir so config.Load finds .orchestra.yml.
	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	// Save and restore the package-level applyProvider flag.
	origProvider := applyProvider
	applyProvider = "nonexistent-provider"
	defer func() { applyProvider = origProvider }()

	// Use a fake query so we pass the early check.
	err := runApply(applyCmd, []string{"test query"})
	if err == nil {
		t.Fatal("expected error for unknown --provider, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent-provider") {
		t.Fatalf("expected error to mention provider name, got: %v", err)
	}
	if !strings.Contains(err.Error(), "myprovider") {
		t.Fatalf("expected error to mention available providers, got: %v", err)
	}
}

// TestRunApply_ProviderFlagKnown_LookupOnly verifies that provider lookup
// succeeds (no "not found in providers:" error) when the name exists in config.
// This directly tests the cfg.FindProvider call and cfg.LLM assignment logic
// without exercising the full agent run.
func TestRunApply_ProviderFlagKnown_LookupOnly(t *testing.T) {
	cfg := &config.ProjectConfig{
		Providers: map[string]config.LLMConfig{
			"myprovider": {
				APIBase:  "http://other:8000/v1",
				Model:    "other-model",
				TimeoutS: 30,
			},
		},
		LLM: config.LLMConfig{Model: "default-model"},
	}

	provCfg, ok := cfg.FindProvider("myprovider")
	if !ok {
		t.Fatal("expected FindProvider to succeed for 'myprovider'")
	}
	// Simulate what runApply does: override cfg.LLM with the named provider.
	cfg.LLM = provCfg
	if cfg.LLM.Model != "other-model" {
		t.Errorf("after provider override, LLM.Model = %q, want %q", cfg.LLM.Model, "other-model")
	}
	if cfg.LLM.APIBase != "http://other:8000/v1" {
		t.Errorf("after provider override, LLM.APIBase = %q, want %q", cfg.LLM.APIBase, "http://other:8000/v1")
	}
}

func TestResolvePatchOutputPath(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ProjectConfig{
		ProjectRoot: tmp,
		Apply:       config.ApplyConfig{PatchDir: ".orchestra/patches"},
	}
	abs, err := resolvePatchOutputPath(cfg, tmp, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(abs, ".patch") {
		t.Fatalf("expected .patch suffix, got %s", abs)
	}
	explicit := filepath.Join("out", "custom.patch")
	abs2, err := resolvePatchOutputPath(cfg, tmp, explicit)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(abs2, filepath.Join("out", "custom.patch")) && !strings.Contains(abs2, "custom.patch") {
		t.Fatalf("unexpected path: %s", abs2)
	}
}
