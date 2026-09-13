//go:build windows

package lsp

import (
	"golang.org/x/sys/windows"
)

// longWorkspacePath expands an 8.3 short path to its full form.
//
// Windows keeps a legacy short name for every path component — "KorsunAndrii"
// is also "KORSUN~1" — and the two spellings name the same directory while
// comparing unequal as strings. TEMP is commonly set to the short form, so
// anything rooted under a temp directory arrives short.
//
// A language server does not compare them either. Handed a short root, gopls
// normalises it its own way and then places every file in the workspace
// OUTSIDE it, answering each request with
//
//	No active builds contain <file>: consider opening a new workspace folder
//	containing it
//
// which is a warning, not an error — so the agent's diagnostic filter drops it
// and the model is told nothing at all. Measured: four real-LSP cases against
// gopls pass under a long root and two fail under a short one, including the
// ordinary case of editing a file that was already in the module.
//
// On failure the original is returned: a path that cannot be expanded (it may
// not exist yet) is still the best spelling available.
func longWorkspacePath(path string) string {
	if path == "" {
		return path
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return path
	}
	n, err := windows.GetLongPathName(p, nil, 0)
	if err != nil || n == 0 {
		return path
	}
	buf := make([]uint16, n)
	n, err = windows.GetLongPathName(p, &buf[0], n)
	if err != nil || n == 0 {
		return path
	}
	return windows.UTF16ToString(buf[:n])
}
