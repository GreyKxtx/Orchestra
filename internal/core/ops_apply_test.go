package core_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
	"github.com/orchestra/orchestra/internal/core"
	"github.com/orchestra/orchestra/internal/ops"
)

func TestOpsApply_NotInitialized(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig(root)
	if err := config.Save(filepath.Join(root, ".orchestra.yml"), cfg); err != nil {
		t.Fatalf("Save config failed: %v", err)
	}

	c, err := core.New(root, core.Options{})
	if err != nil {
		t.Fatalf("New core failed: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	_, err = c.OpsApply(context.Background(), core.OpsApplyParams{
		Ops: []ops.AnyOp{},
	})
	if err == nil {
		t.Fatal("expected NotInitialized error")
	}
}
