// Package toolpath resolves workspace-relative paths with traversal checks.
package toolpath

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/orchestra/orchestra/protocol"
)

// ResolveWorkspacePath resolves a relative path within workspace with realpath-based security checks.
// Returns absolute path, relative path (with forward slashes), and error.
func ResolveWorkspacePath(workspaceRoot, p string) (abs string, relSlash string, _ error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", "", protocol.NewError(protocol.InvalidLLMOutput, "path is empty", nil)
	}

	if filepath.IsAbs(p) {
		abs = filepath.Clean(p)
	} else {
		abs = filepath.Clean(filepath.Join(workspaceRoot, filepath.FromSlash(p)))
	}

	rel, err := filepath.Rel(workspaceRoot, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", "", protocol.NewError(protocol.PathTraversal, "path escapes workspace", map[string]any{
			"path": p,
		})
	}

	rootAbs, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", "", fmt.Errorf("failed to get absolute workspace root: %w", err)
	}
	rootReal := rootAbs
	if rp, err := filepath.EvalSymlinks(rootAbs); err == nil {
		rootReal = rp
	}

	absAbs, err := filepath.Abs(abs)
	if err != nil {
		return "", "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	if realAbs, err := filepath.EvalSymlinks(absAbs); err == nil {
		if !IsWithinRoot(rootReal, realAbs) {
			return "", "", protocol.NewError(protocol.PathTraversal, "path escapes workspace (via symlink/junction)", map[string]any{
				"path":           p,
				"abs_path":       absAbs,
				"real_path":      realAbs,
				"workspace":      rootAbs,
				"workspace_real": rootReal,
			})
		}
	} else if os.IsNotExist(err) {
		dir := absAbs
		for {
			parentDir := filepath.Dir(dir)
			if parentDir == dir || parentDir == "." || parentDir == string(os.PathSeparator) {
				break
			}
			if SamePath(parentDir, rootAbs) {
				break
			}
			if st, statErr := os.Stat(parentDir); statErr == nil && st.IsDir() {
				if realDir, eerr := filepath.EvalSymlinks(parentDir); eerr == nil {
					if !IsWithinRoot(rootReal, realDir) {
						return "", "", protocol.NewError(protocol.PathTraversal, "path escapes workspace (via symlink/junction in parent)", map[string]any{
							"path":      p,
							"parent":    parentDir,
							"real_path": realDir,
						})
					}
				}
				break
			}
			dir = parentDir
		}
	} else {
		return "", "", protocol.NewError(protocol.PathTraversal, "cannot resolve path (symlink/junction)", map[string]any{
			"path":  p,
			"error": err.Error(),
		})
	}

	relSlash = filepath.ToSlash(rel)
	return absAbs, relSlash, nil
}

// IsWithinRoot checks if targetAbs is within rootAbs using realpath comparison.
func IsWithinRoot(rootAbs, targetAbs string) bool {
	r := filepath.Clean(rootAbs)
	t := filepath.Clean(targetAbs)

	if runtime.GOOS == "windows" {
		r = strings.TrimPrefix(r, `\\?\`)
		t = strings.TrimPrefix(t, `\\?\`)
		r = strings.ToLower(r)
		t = strings.ToLower(t)
	}

	if r == t {
		return true
	}

	sep := string(os.PathSeparator)
	if !strings.HasSuffix(r, sep) {
		r += sep
	}
	return strings.HasPrefix(t, r)
}

// SamePath compares two paths for equality (case-insensitive on Windows).
func SamePath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
