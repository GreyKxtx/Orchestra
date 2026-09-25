// Package wsview reads the project the way an agent sees it.
//
// Agents stage their edits: the turn keeps them in its overlay, and a task in
// its own layer over its spawner's (internal/tools/fs). The disk holds only
// what was applied. A runtime check that reads the disk judges a different
// project from the one the agents built: a Lead writes the brief its workers
// need, and the spawn gate, reading the disk, finds none (ORC-6).
//
// So the checks take a View, and the runtime hands them the view of the agent
// the check is about: its own staged changes over its owners', over the disk.
package wsview

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// View reads project files by slash path relative to the project root.
type View interface {
	// ReadFile returns the file's content, or an error that satisfies
	// errors.Is(err, fs.ErrNotExist) when there is no such file.
	ReadFile(rel string) ([]byte, error)
}

// Disk is the view of a project root with nothing staged.
type Disk string

// ReadFile reads rel from under the root.
func (d Disk) ReadFile(rel string) ([]byte, error) {
	return os.ReadFile(filepath.Join(string(d), filepath.FromSlash(Clean(rel))))
}

// Exists reports whether v has a file at rel.
func Exists(v View, rel string) bool {
	_, err := v.ReadFile(rel)
	return err == nil
}

// Clean normalizes a project-relative path to the slash form staged files are
// keyed by.
func Clean(rel string) string {
	p := path.Clean(strings.ReplaceAll(strings.TrimSpace(rel), `\`, "/"))
	return strings.TrimPrefix(p, "./")
}
