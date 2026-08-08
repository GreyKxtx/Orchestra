package provision

import (
	"github.com/orchestra/orchestra/internal/lsp/registry"
)

// initFallbackIDs is the polyglot default when workspace detect finds nothing
// (empty repo). Matches docs/architecture/lsp-auto-provision.md phase C.
var initFallbackIDs = []string{"gopls", "typescript-language-server", "basedpyright"}

// InitServerSpecs returns active LSP server specs for orchestra init.
// Uses workspace detect; falls back to go + typescript + python recipes.
func InitServerSpecs(workspaceRoot string) []ServerSpec {
	detected := Detect(workspaceRoot)
	if len(detected) == 0 {
		for _, id := range initFallbackIDs {
			if e, ok := registry.ByID(id); ok {
				detected = append(detected, e)
			}
		}
	}
	return MergeServers(nil, detected)
}
