package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/orchestra/orchestra/internal/config"
)

func testStore(t *testing.T) (*configStore, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".orchestra.yml")
	return newConfigStore(path, dir), path
}

func TestConfigStore_MutatePersists(t *testing.T) {
	store, path := testStore(t)

	if err := store.Mutate(func(cfg *config.ProjectConfig) error {
		cfg.LLM.Model = "test-model"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.Model != "test-model" {
		t.Fatalf("model=%q", cfg.LLM.Model)
	}
}

func TestConfigStore_MutateUnchangedSkipsSave(t *testing.T) {
	store, path := testStore(t)

	err := store.Mutate(func(cfg *config.ProjectConfig) error {
		return errConfigUnchanged
	})
	if !errors.Is(err, errConfigUnchanged) {
		t.Fatalf("err=%v, want errConfigUnchanged", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatal("no-op mutate must not create the config file")
	}
}

func TestConfigStore_MutateExistingFailsWithoutFile(t *testing.T) {
	store, _ := testStore(t)

	if err := store.MutateExisting(func(cfg *config.ProjectConfig) error {
		t.Fatal("callback must not run when the file is missing")
		return nil
	}); err == nil {
		t.Fatal("expected load error for missing config")
	}
}

func TestConfigStore_EmptyPathFails(t *testing.T) {
	store := newConfigStore("", "")
	if err := store.Mutate(func(cfg *config.ProjectConfig) error { return nil }); err == nil {
		t.Fatal("empty path must be rejected")
	}
}

// Concurrent read-modify-write cycles must not drop each other's fields —
// this is exactly the interleaving that scattered config.Load→Save call
// sites allowed before configStore.
func TestConfigStore_ConcurrentMutatesKeepAllWrites(t *testing.T) {
	store, path := testStore(t)

	const n = 16
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = store.Mutate(func(cfg *config.ProjectConfig) error {
				if cfg.Providers == nil {
					cfg.Providers = map[string]config.LLMConfig{}
				}
				name := fmt.Sprintf("prov-%d", i)
				cfg.Providers[name] = config.LLMConfig{Provider: name}
				return nil
			})
		}(i)
	}
	wg.Wait()

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("prov-%d", i)
		if _, ok := cfg.Providers[name]; !ok {
			t.Fatalf("write %s was lost", name)
		}
	}
}
