package tools

import (
	"github.com/orchestra/orchestra/internal/tools/toolpath"
)

// resolveWorkspacePath resolves a relative path within workspace with realpath-based security checks.
func resolveWorkspacePath(workspaceRoot, p string) (abs string, relSlash string, err error) {
	return toolpath.ResolveWorkspacePath(workspaceRoot, p)
}
