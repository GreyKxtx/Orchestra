package config

import (
	"fmt"
	"strings"
)

// Memory modes the yaml accepts. internal/memory defines what each one does
// and takes its names from here, so the two cannot drift.
const (
	MemoryModeEager  = "eager"
	MemoryModeLazy   = "lazy"
	MemoryModeHybrid = "hybrid"
)

func (c *ProjectConfig) validateMemory() error {
	mode := strings.ToLower(strings.TrimSpace(c.Memory.Mode))
	if mode == "" {
		return nil
	}
	switch mode {
	case MemoryModeEager, MemoryModeLazy, MemoryModeHybrid:
		return nil
	default:
		return fmt.Errorf("memory.mode must be %q, %q, or %q, got %q",
			MemoryModeEager, MemoryModeLazy, MemoryModeHybrid, c.Memory.Mode)
	}
}
