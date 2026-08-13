package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const overlayTestMain = `project_root: .
llm:
  api_base: https://openrouter.ai/api/v1
  model: qwen2.5-coder-7b
providers:
  openrouter:
    api_base: https://openrouter.ai/api/v1
    model: anthropic/claude-sonnet
`

const overlayTestLocal = `llm:
  api_key: sk-secret-main
providers:
  openrouter:
    api_key: sk-secret-provider
`

func writeOverlayFixture(t *testing.T) (dir, cfgPath string) {
	t.Helper()
	dir = t.TempDir()
	cfgPath = filepath.Join(dir, ".orchestra.yml")
	if err := os.WriteFile(cfgPath, []byte(overlayTestMain), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, LocalOverlayName), []byte(overlayTestLocal), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, cfgPath
}

// Load must deep-merge the overlay: secrets come from local, everything else
// from the main file — including sibling keys of an overridden provider.
func TestLoad_MergesLocalOverlay(t *testing.T) {
	_, cfgPath := writeOverlayFixture(t)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LLM.APIKey != "sk-secret-main" {
		t.Errorf("llm.api_key: want overlay secret, got %q", cfg.LLM.APIKey)
	}
	if cfg.LLM.APIBase != "https://openrouter.ai/api/v1" {
		t.Errorf("llm.api_base lost during merge: %q", cfg.LLM.APIBase)
	}
	p, ok := cfg.Providers["openrouter"]
	if !ok {
		t.Fatal("providers.openrouter missing after merge")
	}
	if p.APIKey != "sk-secret-provider" {
		t.Errorf("provider api_key: want overlay secret, got %q", p.APIKey)
	}
	if p.Model != "anthropic/claude-sonnet" {
		t.Errorf("provider model clobbered by leaf merge: %q", p.Model)
	}
}

// Save must never persist overlay-owned values into the main file: a settings
// round-trip (Load → Save) with an overlay present must keep secrets out of
// .orchestra.yml while leaving the overlay file untouched.
func TestSave_MasksLocalOverlaySecrets(t *testing.T) {
	dir, cfgPath := writeOverlayFixture(t)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Simulate a legitimate settings change alongside the merged secrets.
	cfg.LLM.Model = "new-model"
	if err := Save(cfgPath, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-secret-main") || strings.Contains(string(raw), "sk-secret-provider") {
		t.Fatalf("overlay secret leaked into main config:\n%s", raw)
	}
	if !strings.Contains(string(raw), "new-model") {
		t.Errorf("legitimate change lost on save:\n%s", raw)
	}

	// Overlay stays intact and a reload restores the merged view.
	local, err := os.ReadFile(filepath.Join(dir, LocalOverlayName))
	if err != nil {
		t.Fatal(err)
	}
	if string(local) != overlayTestLocal {
		t.Errorf("overlay file modified by Save:\n%s", local)
	}
	reloaded, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.LLM.APIKey != "sk-secret-main" || reloaded.LLM.Model != "new-model" {
		t.Errorf("round-trip lost data: api_key=%q model=%q", reloaded.LLM.APIKey, reloaded.LLM.Model)
	}
}

// Without an overlay, Load/Save must behave exactly as before.
func TestLoadSave_NoOverlayUnchanged(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".orchestra.yml")
	if err := os.WriteFile(cfgPath, []byte(overlayTestMain), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.LLM.APIKey = "sk-typed-in-ui"
	if err := Save(cfgPath, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(raw), "sk-typed-in-ui") {
		t.Errorf("without overlay the key must persist to the main file:\n%s", raw)
	}
}
