//go:build !windows

package lsp

// longWorkspacePath is a no-op off Windows: 8.3 short names are a Windows
// filesystem feature, and every other platform already has one spelling per
// path. See longpath_windows.go for what this exists to fix.
func longWorkspacePath(path string) string { return path }
