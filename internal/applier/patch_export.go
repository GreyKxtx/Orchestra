package applier

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/aymanbagabas/go-udiff"
	"github.com/orchestra/orchestra/internal/fsutil"
)

// UnifiedPatch builds a git-apply-compatible unified diff from FileDiffs.
// Paths in diffs are treated as workspace-relative (slash or OS separators).
// Empty diffs yield an empty string (not an error).
func UnifiedPatch(diffs []FileDiff) string {
	if len(diffs) == 0 {
		return ""
	}
	var b strings.Builder
	for i, d := range diffs {
		rel := filepath.ToSlash(strings.TrimSpace(d.Path))
		if rel == "" {
			rel = "unknown"
		}
		oldName := "a/" + rel
		newName := "b/" + rel
		before := d.Before
		after := d.After
		// Match git conventions for create/delete.
		if before == "" && after != "" {
			oldName = "/dev/null"
		}
		if before != "" && after == "" {
			newName = "/dev/null"
		}
		chunk := udiff.Unified(oldName, newName, before, after)
		if chunk == "" {
			continue
		}
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
			b.WriteByte('\n')
		}
		b.WriteString(chunk)
		if i < len(diffs)-1 && !strings.HasSuffix(chunk, "\n") {
			b.WriteByte('\n')
		}
	}
	out := b.String()
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}

// WriteUnifiedPatch atomically writes UnifiedPatch(diffs) to path.
// Creates parent directories as needed. No-op (nil error) when diffs is empty
// and path would be empty content — still writes an empty file so callers
// can rely on the path existing when they requested an export.
func WriteUnifiedPatch(path string, diffs []FileDiff) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("applier: empty patch path")
	}
	data := []byte(UnifiedPatch(diffs))
	return fsutil.AtomicWriteFile(path, data, 0o600)
}

// DefaultPatchPath returns <dir>/orchestra-<utc-timestamp>.patch.
// dir may be absolute or relative; caller resolves against project root.
func DefaultPatchPath(dir string) string {
	if strings.TrimSpace(dir) == "" {
		dir = ".orchestra/patches"
	}
	ts := time.Now().UTC().Format("20060102T150405")
	return filepath.Join(dir, "orchestra-"+ts+".patch")
}
