package tui

import (
	"errors"
	"fmt"
	"sync"

	"github.com/orchestra/orchestra/internal/config"
)

// errConfigUnchanged is returned by a Mutate callback to signal "nothing to
// persist": Mutate skips the Save and propagates this sentinel so callers can
// distinguish a no-op from a real failure.
var errConfigUnchanged = errors.New("config unchanged")

// configStore is the single write path to .orchestra.yml from the TUI.
// Every read-modify-write cycle goes through Mutate under one mutex, so two
// concurrent tea.Cmd goroutines (settings save, limits probe, orchestra
// dialog, MCP edit…) can never interleave Load→Save and silently drop each
// other's fields. Plain reads elsewhere stay safe because config.Save writes
// atomically (temp file → rename).
type configStore struct {
	mu            sync.Mutex
	path          string
	workspaceRoot string
}

func newConfigStore(path, workspaceRoot string) *configStore {
	return &configStore{path: path, workspaceRoot: workspaceRoot}
}

// Mutate loads the current config (falling back to defaults when the file is
// missing or unreadable), applies fn and saves the result. Serialized by the
// store mutex. fn may return errConfigUnchanged to skip the save.
func (s *configStore) Mutate(fn func(cfg *config.ProjectConfig) error) error {
	if s == nil || s.path == "" {
		return fmt.Errorf("no .orchestra.yml path configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := config.Load(s.path)
	if err != nil || cfg == nil {
		cfg = config.DefaultConfig(s.workspaceRoot)
		cfg.ProjectRoot = s.workspaceRoot
	}
	if err := fn(cfg); err != nil {
		return err
	}
	return config.Save(s.path, cfg)
}

// MutateExisting is Mutate for flows that must not resurrect a broken or
// missing config file with defaults (e.g. MCP edits): a load failure is
// returned to the caller instead of being papered over.
func (s *configStore) MutateExisting(fn func(cfg *config.ProjectConfig) error) error {
	if s == nil || s.path == "" {
		return fmt.Errorf("no .orchestra.yml path configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := config.Load(s.path)
	if err != nil {
		return err
	}
	if err := fn(cfg); err != nil {
		return err
	}
	return config.Save(s.path, cfg)
}
