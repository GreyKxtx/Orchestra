// Package relpath validates and canonicalises workspace-relative paths.
//
// Two callers each carried their own copy of the same lexical checks
// (empty / "." / "..") + path-traversal rejection:
//   - internal/resolver/external_patches.go::normalizeRelPath
//   - internal/applier/ops_applier.go::safeAbsPath
//
// Extracted in S5 of the audit ledger (Sprint 6) so the wire-error
// shape (InvalidLLMOutput / PathTraversal) and the "no trailing slash,
// no dot, no escape" semantics are defined in one place.
package relpath

import (
	"path"
	"strings"

	"github.com/orchestra/orchestra/protocol"
)

// Normalize trims and canonicalises a workspace-relative path. Returns
// the canonical slash-form ("path/to/file"), or a protocol.Error with
// the appropriate code (InvalidLLMOutput for empty / dot, PathTraversal
// for ".." escapes).
//
// Always uses slash separators (OS-independent): callers pass LLM /
// JSON paths that may contain Windows backslashes; the result is always
// forward-slash form suitable for workspace-relative wire formats.
//
// Does NOT touch the filesystem. Callers that need symlink protection
// must layer that on top (see applier.safeAbsPath).
func Normalize(p string) (string, *protocol.Error) {
	p = strings.TrimSpace(p)
	// Force slash form before Clean so Windows filepath.Clean cannot
	// re-introduce backslashes into the canonical result.
	p = strings.ReplaceAll(p, `\`, `/`)
	if p == "" {
		return "", protocol.NewError(protocol.InvalidLLMOutput, "path is empty", nil)
	}
	p = path.Clean(p)
	if p == "." {
		return "", protocol.NewError(protocol.InvalidLLMOutput, "path is invalid", map[string]any{"path": p})
	}
	if p == ".." || strings.HasPrefix(p, "../") {
		return "", protocol.NewError(protocol.PathTraversal, "path escapes workspace", map[string]any{"path": p})
	}
	return p, nil
}
