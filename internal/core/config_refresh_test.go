package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/llm"
)

type refreshStubLLM struct{}

func (refreshStubLLM) Complete(context.Context, llm.CompleteRequest) (*llm.CompleteResponse, error) {
	return &llm.CompleteResponse{}, nil
}

func (refreshStubLLM) Plan(context.Context, string) (string, error) {
	return "{}", nil
}

// TestRefreshConfigIfChanged verifies the shared-config invariant: an external
// edit to .orchestra.yml (e.g. the TUI in a terminal) is picked up by a
// long-running core process before the next RPC is dispatched.
func TestRefreshConfigIfChanged(t *testing.T) {
	root := t.TempDir()
	cfgPath := filepath.Join(root, ".orchestra.yml")

	cfg := config.DefaultConfig(root)
	cfg.LLM.APIBase = "http://127.0.0.1:9/v1"
	cfg.LLM.APIKey = "k"
	cfg.LLM.Model = "old-model"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}

	c, err := New(root, Options{LLMClient: refreshStubLLM{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if c.cfg.LLM.Model != "old-model" {
		t.Fatalf("initial model = %q", c.cfg.LLM.Model)
	}

	// External edit (simulates TUI writing the same file).
	ext, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	ext.LLM.Model = "new-model"
	if err := config.Save(cfgPath, ext); err != nil {
		t.Fatal(err)
	}
	// Force a distinct mtime even on coarse-granularity filesystems.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(cfgPath, future, future); err != nil {
		t.Fatal(err)
	}

	c.RefreshConfigIfChanged()

	if c.cfg.LLM.Model != "new-model" {
		t.Fatalf("model after refresh = %q, want new-model", c.cfg.LLM.Model)
	}
}

// TestRefreshConfigNoChange ensures an untouched file does not trigger a reload
// (the in-memory pointer must stay identical).
func TestRefreshConfigNoChange(t *testing.T) {
	root := t.TempDir()
	cfgPath := filepath.Join(root, ".orchestra.yml")
	cfg := config.DefaultConfig(root)
	cfg.LLM.APIBase = "http://127.0.0.1:9/v1"
	cfg.LLM.Model = "m"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	c, err := New(root, Options{LLMClient: refreshStubLLM{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	before := c.cfg
	c.RefreshConfigIfChanged()
	if c.cfg != before {
		t.Fatal("config reloaded without an external change")
	}
}

// TestRefreshSkippedDuringTurn: a running agent turn (runMu held) must not be
// raced by a config hot-swap; refresh is silently skipped.
func TestRefreshSkippedDuringTurn(t *testing.T) {
	root := t.TempDir()
	cfgPath := filepath.Join(root, ".orchestra.yml")
	cfg := config.DefaultConfig(root)
	cfg.LLM.APIBase = "http://127.0.0.1:9/v1"
	cfg.LLM.Model = "old-model"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	c, err := New(root, Options{LLMClient: refreshStubLLM{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ext, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	ext.LLM.Model = "new-model"
	if err := config.Save(cfgPath, ext); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(cfgPath, future, future); err != nil {
		t.Fatal(err)
	}

	c.runMu.Lock()
	c.RefreshConfigIfChanged()
	c.runMu.Unlock()
	if c.cfg.LLM.Model != "old-model" {
		t.Fatal("config must not be swapped while a turn holds runMu")
	}

	// After the turn releases runMu the next RPC picks the change up.
	c.RefreshConfigIfChanged()
	if c.cfg.LLM.Model != "new-model" {
		t.Fatalf("model after unlock = %q, want new-model", c.cfg.LLM.Model)
	}
}
