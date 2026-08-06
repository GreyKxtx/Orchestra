package lsp

// ContentProvider supplies in-memory file content that overrides disk reads
// for LSP operations (e.g. dry-run staging overlay).
type ContentProvider interface {
	// EffectiveContent returns staged/in-memory content for relPath when the
	// provider has a newer view than disk. ok=false → Manager reads from disk.
	EffectiveContent(relPath string) (content string, ok bool)
}

// ContentProviderFunc adapts a function to ContentProvider.
type ContentProviderFunc func(relPath string) (content string, ok bool)

func (f ContentProviderFunc) EffectiveContent(relPath string) (content string, ok bool) {
	return f(relPath)
}

// StagedPathsProvider lists in-memory staged paths (dry-run overlay).
// Optional extension of ContentProvider; used to re-open documents after LSP wake.
type StagedPathsProvider interface {
	ListStagedPaths() []string
}
