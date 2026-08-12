package tui

import (
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/ui/tui/view"
)

func testSettingsResult() view.SettingsDialogResult {
	return view.SettingsDialogResult{
		Provider:       view.ProviderEntry{Key: "vllm", Endpoint: "http://localhost:8000"},
		Model:          view.ModelEntry{ID: "qwen3-32b"},
		APIKey:         "sk-test",
		Temperature:    0.4,
		MaxTokens:      8192,
		NumCtx:         32768,
		TimeoutS:       120,
		EnableThinking: true,
	}
}

func TestApplySettingsResult_WritesAllLayers(t *testing.T) {
	cfg := config.DefaultConfig(t.TempDir())
	applySettingsResult(cfg, testSettingsResult())

	// Main llm: block.
	if cfg.LLM.Provider != "vllm" || cfg.LLM.Model != "qwen3-32b" {
		t.Fatalf("llm: provider=%q model=%q", cfg.LLM.Provider, cfg.LLM.Model)
	}
	if cfg.LLM.APIKey != "sk-test" || cfg.LLM.MaxTokens != 8192 || cfg.LLM.TimeoutS != 120 {
		t.Fatalf("llm: key=%q maxTokens=%d timeout=%d", cfg.LLM.APIKey, cfg.LLM.MaxTokens, cfg.LLM.TimeoutS)
	}
	if got := cfg.LLM.ExtraBody["num_ctx"]; got != int64(32768) {
		t.Fatalf("num_ctx=%v", got)
	}

	// providers: mirror keeps the key for named lookups (orchestra/router).
	pc, ok := cfg.Providers["vllm"]
	if !ok || pc.APIKey != "sk-test" || pc.Model != "qwen3-32b" {
		t.Fatalf("providers mirror: ok=%v pc=%+v", ok, pc)
	}

	// Per-model preset for re-hydration.
	preset, ok := cfg.LLM.ModelPresets["qwen3-32b"]
	if !ok || preset.NumCtx != 32768 || preset.EnableThinking == nil || !*preset.EnableThinking {
		t.Fatalf("preset: ok=%v %+v", ok, preset)
	}
}

func TestApplySettingsResult_EmptyKeyNeverWipesExisting(t *testing.T) {
	cfg := config.DefaultConfig(t.TempDir())
	cfg.LLM.APIKey = "sk-existing"

	r := testSettingsResult()
	r.APIKey = "   "
	applySettingsResult(cfg, r)

	if cfg.LLM.APIKey != "sk-existing" {
		t.Fatalf("existing key wiped: %q", cfg.LLM.APIKey)
	}
	if pc := cfg.Providers["vllm"]; pc.APIKey != "sk-existing" {
		t.Fatalf("providers mirror lost key: %q", pc.APIKey)
	}
}

func TestApplyOrchestraResult_RolesTiersAndEmbed(t *testing.T) {
	cfg := config.DefaultConfig(t.TempDir())
	applyOrchestraResult(cfg, view.OrchestraDialogResult{
		Roles: []view.OrchestraRoleDraft{
			{Key: view.OrchestraRolePlanner, Provider: "openai", Model: "gpt-4.1"},
			{Key: view.OrchestraRoleLead, Provider: "vllm-lead", Model: "qwen3-32b"},
			{Key: view.OrchestraRoleMicro, Provider: "vllm-micro", Model: "qwen3-4b"},
			{Key: view.OrchestraRoleEmbed, Provider: "ollama", Model: "nomic-embed-text"},
		},
		Named: map[string]view.OrchestraNamedProvider{
			"vllm-lead": {Key: "vllm-lead", APIBase: "http://localhost:8001", APIKey: "k1"},
		},
	})

	if cfg.Orchestra.Planner.Provider != "openai" || cfg.Orchestra.Planner.Model != "gpt-4.1" {
		t.Fatalf("planner=%+v", cfg.Orchestra.Planner)
	}
	if cfg.Embed.Provider != "ollama" || cfg.Embed.Model != "nomic-embed-text" {
		t.Fatalf("embed=%+v", cfg.Embed)
	}
	// Named snapshot persisted with URL + key.
	lead, ok := cfg.Providers["vllm-lead"]
	if !ok || lead.APIKey != "k1" || lead.APIBase == "" {
		t.Fatalf("named provider: ok=%v %+v", ok, lead)
	}
	// Tier roles land in the tier list.
	tierNames := map[string]string{}
	for _, tier := range cfg.Orchestra.Tiers {
		tierNames[tier.Name] = tier.Model
	}
	if tierNames["lead"] != "qwen3-32b" || tierNames["micro"] != "qwen3-4b" {
		t.Fatalf("tiers=%v", tierNames)
	}
}

func TestHandleSettingsDialog_SavePersistsToDisk(t *testing.T) {
	a := testChromeApp(t)
	dir := t.TempDir()
	path := filepath.Join(dir, ".orchestra.yml")
	a.cfg.ConfigPath = path
	a.cfgStore = newConfigStore(path, dir)
	a.pushDialog(view.NewSettingsDialog(view.ProviderEntry{Key: "vllm"}, view.ModelEntry{ID: "m"}))

	_, cmd := a.handleSettingsDialog(view.SettingsDialogMsg{Result: testSettingsResult()})
	if cmd == nil {
		t.Fatal("save must return a persist command")
	}
	if len(a.dialogStack) != 0 {
		t.Fatal("save must close the dialog stack")
	}

	msg := cmd()
	saved, ok := msg.(settingsSavedMsg)
	if !ok || saved.err != nil {
		t.Fatalf("msg=%#v", msg)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.Model != "qwen3-32b" || cfg.LLM.Provider != "vllm" {
		t.Fatalf("config not persisted: %+v", cfg.LLM)
	}
}

func TestHandleSettingsDialog_CancelDropsPendingKey(t *testing.T) {
	a := testChromeApp(t)
	a.pendingAPIKey = "sk-tmp"
	a.pushDialog(view.NewSettingsDialog(view.ProviderEntry{Key: "vllm"}, view.ModelEntry{ID: "m"}))

	_, cmd := a.handleSettingsDialog(view.SettingsDialogMsg{Cancel: true})
	if cmd != nil {
		t.Fatal("cancel must not persist anything")
	}
	if a.pendingAPIKey != "" {
		t.Fatal("pending API key must be dropped on cancel")
	}
	if len(a.dialogStack) != 0 {
		t.Fatal("cancel must pop the dialog")
	}
}

func TestDialogStack_PushPopTop(t *testing.T) {
	a := testChromeApp(t)
	if a.topDialog() != nil {
		t.Fatal("fresh stack must be empty")
	}
	d1 := view.NewSettingsDialog(view.ProviderEntry{Key: "p1"}, view.ModelEntry{ID: "m1"})
	d2 := view.NewSettingsDialog(view.ProviderEntry{Key: "p2"}, view.ModelEntry{ID: "m2"})
	a.pushDialog(d1)
	a.pushDialog(d2)
	if a.topDialog() != view.Dialog(d2) {
		t.Fatal("top must be the last pushed dialog")
	}
	a.popDialog()
	if a.topDialog() != view.Dialog(d1) {
		t.Fatal("pop must reveal the previous dialog")
	}
	a.popDialog()
	a.popDialog() // pop on empty must not panic
	if a.topDialog() != nil {
		t.Fatal("stack must be empty")
	}
}
