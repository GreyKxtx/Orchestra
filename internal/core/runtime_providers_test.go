package core_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/core"
	"github.com/orchestra/orchestra/llm"
)

func TestRuntimeListProviders_CatalogAndReady(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig(root)
	cfg.ProjectRoot = root
	cfg.LLM.Provider = "openai"
	cfg.LLM.APIBase = "https://api.openai.com/v1"
	cfg.LLM.APIKey = "sk-test"
	cfg.LLM.Model = "gpt-4o"
	cfg.Providers = map[string]config.LLMConfig{
		"work": {
			Provider: "vllm",
			APIBase:  "http://127.0.0.1:8000/v1",
			Model:    "local-model",
		},
	}
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatal(err)
	}

	c, err := core.New(root, core.Options{LLMClient: stubLLM{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	res, err := c.RuntimeListProviders(context.Background(), core.RuntimeListProvidersParams{})
	if err != nil {
		t.Fatal(err)
	}
	if res.ActiveProvider != "openai" || res.ActiveModel != "gpt-4o" {
		t.Fatalf("active: provider=%q model=%q", res.ActiveProvider, res.ActiveModel)
	}
	if len(res.Providers) < len(llm.ProviderCatalog) {
		t.Fatalf("expected at least catalog size, got %d", len(res.Providers))
	}

	var openai, ollama, work *core.RuntimeProviderEntry
	for i := range res.Providers {
		p := &res.Providers[i]
		switch p.Key {
		case "openai":
			openai = p
		case "ollama":
			ollama = p
		case "work":
			work = p
		}
	}
	if openai == nil || !openai.Active || !openai.Ready || !openai.APIKeySet {
		t.Fatalf("openai entry: %+v", openai)
	}
	if ollama == nil || !ollama.Ready || ollama.Configured {
		t.Fatalf("ollama should be ready (default localhost) but not configured: %+v", ollama)
	}
	if work == nil || !work.Named || !work.Ready {
		t.Fatalf("named work entry: %+v", work)
	}
}

func TestRuntimeListProviders_IncludeSecrets(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig(root)
	cfg.ProjectRoot = root
	cfg.LLM.Provider = "openai"
	cfg.LLM.APIBase = "https://api.openai.com/v1"
	cfg.LLM.APIKey = "sk-secret-key"
	cfg.LLM.Model = "gpt-4o"
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatal(err)
	}

	c, err := core.New(root, core.Options{LLMClient: stubLLM{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	without, err := c.RuntimeListProviders(context.Background(), core.RuntimeListProvidersParams{})
	if err != nil {
		t.Fatal(err)
	}
	includeSecrets := true
	with, err := c.RuntimeListProviders(context.Background(), core.RuntimeListProvidersParams{
		IncludeSecrets: &includeSecrets,
	})
	if err != nil {
		t.Fatal(err)
	}

	var openaiNo, openaiYes *core.RuntimeProviderEntry
	for i := range without.Providers {
		if without.Providers[i].Key == "openai" {
			openaiNo = &without.Providers[i]
		}
	}
	for i := range with.Providers {
		if with.Providers[i].Key == "openai" {
			openaiYes = &with.Providers[i]
		}
	}
	if openaiNo == nil || openaiNo.APIKey != "" {
		t.Fatalf("without secrets: api_key should be empty, got %q", openaiNo.APIKey)
	}
	if openaiYes == nil || openaiYes.APIKey != "sk-secret-key" {
		t.Fatalf("with secrets: api_key=%q", openaiYes.APIKey)
	}
}
