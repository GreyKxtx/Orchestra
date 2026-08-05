package config

import (
	"fmt"
	"strings"

	"github.com/orchestra/orchestra/internal/memory"
)

// Resolve returns a normalized memory.Config from YAML settings.
func (m MemoryConfig) Resolve() memory.Config {
	cfg := memory.DefaultConfig()
	if m.InjectKB > 0 {
		cfg.InjectKB = m.InjectKB
	}
	if m.LazyKB > 0 {
		cfg.LazyKB = m.LazyKB
	}
	if strings.TrimSpace(m.Mode) != "" {
		cfg.Mode = strings.TrimSpace(m.Mode)
	}
	if m.GlobalEnabled != nil {
		cfg.GlobalEnabled = *m.GlobalEnabled
	}
	if m.SessionEnabled != nil {
		cfg.SessionEnabled = *m.SessionEnabled
	}
	if m.MaxAgentKB > 0 {
		cfg.MaxAgentKB = m.MaxAgentKB
	}
	cfg.Normalize()
	return cfg
}

func (c *ProjectConfig) validateMemory() error {
	mode := strings.ToLower(strings.TrimSpace(c.Memory.Mode))
	if mode == "" {
		return nil
	}
	switch mode {
	case memory.ModeEager, memory.ModeLazy, memory.ModeHybrid:
		return nil
	default:
		return fmt.Errorf("memory.mode must be %q, %q, or %q, got %q",
			memory.ModeEager, memory.ModeLazy, memory.ModeHybrid, c.Memory.Mode)
	}
}
