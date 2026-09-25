package core

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
)

// runtime.set_model rebuilt the client with a bare llm.NewClient: after one
// model switch the request log (and the fallback provider and the router)
// were gone for the rest of the process. It builds the whole stack now.
func TestRuntimeSetModel_KeepsTheClientStack(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig(root)
	cfg.ProjectRoot = root
	cfg.LLM.APIBase = "http://127.0.0.1:9/v1"
	cfg.LLM.Model = "old-model"
	cfg.LLM.Provider = "openai"
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatal(err)
	}
	c, err := New(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if llm.LoggerOf(c.llmClient) == nil {
		t.Fatal("the core's first client has a logger")
	}
	persist := false
	if _, err := c.RuntimeSetModel(context.Background(), RuntimeSetModelParams{Model: "new-model", Persist: &persist}); err != nil {
		t.Fatal(err)
	}
	if llm.LoggerOf(c.llmClient) == nil {
		t.Fatal("after a model switch the client writes no request log")
	}
}
