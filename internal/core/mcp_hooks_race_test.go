package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/orchestra/orchestra/internal/config"
)

func newSamplingCore(t *testing.T) (*Core, string) {
	t.Helper()
	root := t.TempDir()
	cfgPath := filepath.Join(root, ".orchestra.yml")
	cfg := config.DefaultConfig(root)
	cfg.LLM.APIBase = "http://127.0.0.1:9/v1"
	cfg.LLM.APIKey = "k"
	cfg.LLM.Model = "m0"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	c, err := New(root, Options{LLMClient: refreshStubLLM{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, cfgPath
}

// The model an MCP server samples with is read from an MCP goroutine — one
// that holds neither runMu nor cfgMu — while runtime.set_model rewrites
// c.llmClient and c.cfg.LLM in place under runMu alone. Reading those fields
// directly is a data race the moment a server samples during a model switch;
// -race is what makes this test fail. The label check makes it fail without
// -race too when a writer forgets to publish the new model.
func TestMCPSampling_ModelResolverIsSafeAgainstSetModel(t *testing.T) {
	c, _ := newSamplingCore(t)
	off := false

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 300; i++ {
			c.mcpHost.model()
		}
	}()
	for i := 1; i <= 300; i++ {
		if _, err := c.RuntimeSetModel(context.Background(), RuntimeSetModelParams{
			Model: fmt.Sprintf("m%d", i), Persist: &off,
		}); err != nil {
			t.Fatal(err)
		}
	}
	<-done

	if _, model := c.mcpHost.model(); model != "m300" {
		t.Fatalf("sampling would use model %q after switching to m300", model)
	}
}

func TestMCPSampling_ModelFollowsConfigureLLMAndDiskRefresh(t *testing.T) {
	c, cfgPath := newSamplingCore(t)
	off := false

	if _, err := c.RuntimeConfigureLLM(context.Background(), RuntimeConfigureLLMParams{
		Model: "configured", Persist: &off,
	}); err != nil {
		t.Fatal(err)
	}
	if _, model := c.mcpHost.model(); model != "configured" {
		t.Fatalf("after runtime.configure_llm sampling would use %q", model)
	}

	// External edit of .orchestra.yml, picked up before the next RPC.
	ext, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	ext.LLM.Model = "from-disk"
	if err := config.Save(cfgPath, ext); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(cfgPath, future, future); err != nil {
		t.Fatal(err)
	}
	c.RefreshConfigIfChanged()
	if _, model := c.mcpHost.model(); model != "from-disk" {
		t.Fatalf("after a disk refresh sampling would use %q", model)
	}
}
