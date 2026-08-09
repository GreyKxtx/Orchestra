package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ResolveInWorkspace resolves p under workspaceRoot with lexical + realpath checks.
// Returns absolute path, workspace-relative slash path, and error on escape.
func ResolveInWorkspace(workspaceRoot, p string) (abs string, relSlash string, err error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", "", fmt.Errorf("path is empty")
	}
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" {
		return "", "", fmt.Errorf("workspace root is empty")
	}

	if filepath.IsAbs(p) {
		abs = filepath.Clean(p)
	} else {
		abs = filepath.Clean(filepath.Join(workspaceRoot, filepath.FromSlash(p)))
	}

	rel, err := filepath.Rel(workspaceRoot, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", "", fmt.Errorf("path escapes workspace: %s", p)
	}

	rootAbs, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", "", fmt.Errorf("workspace root: %w", err)
	}
	rootReal := rootAbs
	if rp, err := filepath.EvalSymlinks(rootAbs); err == nil {
		rootReal = rp
	}

	absAbs, err := filepath.Abs(abs)
	if err != nil {
		return "", "", fmt.Errorf("absolute path: %w", err)
	}

	if realAbs, err := filepath.EvalSymlinks(absAbs); err == nil {
		if !isWithinRoot(rootReal, realAbs) {
			return "", "", fmt.Errorf("path escapes workspace via symlink: %s", p)
		}
	} else if os.IsNotExist(err) {
		dir := absAbs
		for {
			parentDir := filepath.Dir(dir)
			if parentDir == dir || parentDir == "." || parentDir == string(os.PathSeparator) {
				break
			}
			if samePath(parentDir, rootAbs) {
				break
			}
			if st, statErr := os.Stat(parentDir); statErr == nil && st.IsDir() {
				if realDir, eerr := filepath.EvalSymlinks(parentDir); eerr == nil {
					if !isWithinRoot(rootReal, realDir) {
						return "", "", fmt.Errorf("path escapes workspace via parent symlink: %s", p)
					}
				}
				break
			}
			dir = parentDir
		}
	} else {
		return "", "", fmt.Errorf("cannot resolve path %s: %w", p, err)
	}

	return absAbs, filepath.ToSlash(rel), nil
}

func isWithinRoot(rootAbs, targetAbs string) bool {
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

func samePath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
