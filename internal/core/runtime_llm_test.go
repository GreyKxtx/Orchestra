package core_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/core"
	"github.com/orchestra/orchestra/llm"
	"github.com/orchestra/orchestra/protocol"
)

type stubLLM struct{}

func (stubLLM) Complete(context.Context, llm.CompleteRequest) (*llm.CompleteResponse, error) {
	return &llm.CompleteResponse{}, nil
}

func (stubLLM) Plan(context.Context, string) (string, error) {
	return "{}", nil
}

func TestRuntimeSetModel_PersistAndHealth(t *testing.T) {
	root := t.TempDir()
	cfgPath := filepath.Join(root, ".orchestra.yml")
	cfg := config.DefaultConfig(root)
	cfg.ProjectRoot = root
	cfg.LLM.APIBase = "http://127.0.0.1:9/v1"
	cfg.LLM.Model = "old-model"
	cfg.LLM.Provider = "openai"
	cfg.LLM.APIKey = "test-key"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	c, err := core.New(root, core.Options{LLMClient: stubLLM{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	persist := true
	res, err := c.RuntimeSetModel(context.Background(), core.RuntimeSetModelParams{
		Model:   "new-model",
		Persist: &persist,
	})
	if err != nil {
		t.Fatalf("RuntimeSetModel: %v", err)
	}
	if res.Model != "new-model" || !res.Persisted {
		t.Fatalf("unexpected result: %+v", res)
	}
	h := c.Health()
	if h.Model != "new-model" || h.ProtocolVersion != protocol.ProtocolVersion {
		t.Fatalf("health: %+v", h)
	}
	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if loaded.LLM.Model != "new-model" {
		t.Fatalf("disk model=%q", loaded.LLM.Model)
	}
	_ = os.Remove(cfgPath)
}

func TestRuntimeSetModel_EmptyRejected(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig(root)
	cfg.ProjectRoot = root
	cfg.LLM.APIBase = "http://127.0.0.1:9/v1"
	cfg.LLM.Model = "m"
	cfg.LLM.APIKey = "k"
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatal(err)
	}
	c, err := core.New(root, core.Options{LLMClient: stubLLM{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	_, err = c.RuntimeSetModel(context.Background(), core.RuntimeSetModelParams{Model: "  "})
	if err == nil {
		t.Fatal("expected error")
	}
}
