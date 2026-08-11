package core_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/core"
)

func TestRuntimeConfigureLLM_ProviderKeyWithoutModel(t *testing.T) {
	root := t.TempDir()
	cfgPath := filepath.Join(root, ".orchestra.yml")
	cfg := config.DefaultConfig(root)
	cfg.ProjectRoot = root
	cfg.LLM = config.LLMConfig{
		Provider: "vllm",
		APIBase:  "http://127.0.0.1:8000/v1",
		Model:    "qwen/test",
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}

	c, err := core.New(root, core.Options{LLMClient: stubLLM{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	persist := true
	res, err := c.RuntimeConfigureLLM(context.Background(), core.RuntimeConfigureLLMParams{
		Provider: "openrouter",
		APIKey:   "sk-or-test-key",
		Persist:  &persist,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Persisted || !res.APIKeySet {
		t.Fatalf("persisted=%v apiKeySet=%v", res.Persisted, res.APIKeySet)
	}

	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LLM.Provider != "vllm" {
		t.Fatalf("active provider switched: %q", loaded.LLM.Provider)
	}
	pc, ok := loaded.Providers["openrouter"]
	if !ok || pc.APIKey != "sk-or-test-key" {
		t.Fatalf("openrouter entry=%+v ok=%v", pc, ok)
	}
	if !strings.Contains(pc.APIBase, "openrouter.ai") {
		t.Fatalf("api_base=%q", pc.APIBase)
	}
}
