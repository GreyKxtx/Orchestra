package prompt

import (
	"github.com/orchestra/orchestra/internal/memory"
)

const defaultMemoryCap = 8 * 1024 // 8 KiB — kept for tests

// LoadProjectMemory reads project memory and returns a <project_memory> block.
// Deprecated callers should prefer memory.NewStore directly; this wrapper
// preserves the pre-v2 API used in tests.
func LoadProjectMemory(workspaceRoot string, maxBytes int) string {
	cfg := memory.DefaultConfig()
	cfg.Mode = memory.ModeEager
	store := memory.NewStore(workspaceRoot, "", cfg)
	if maxBytes <= 0 {
		maxBytes = defaultMemoryCap
	}
	return store.FormatInject(maxBytes)
}
