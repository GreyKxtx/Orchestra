package memory

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// importMacro matches "@import <path>", the same leading-boundary shape as
// skills' @refs/ macro (internal/skills/refs.go) so "email@import.com" can't
// false-positive.
var importMacro = regexp.MustCompile(`(^|[\s])@import\s+(\S+)`)

// maxImportDepth caps how many files an @import chain may pull in. A doc
// tree that needs more than three hops of indirection is a design problem
// the cap surfaces rather than one Orchestra should silently keep unwinding.
const maxImportDepth = 3

// expandImports replaces every "@import <path>" in content with the target
// file's own trimmed content, resolved relative to dir (the directory of
// the file content came from). A nested @import inside an imported file
// resolves relative to THAT file's own directory, so moving a doc subtree
// doesn't break the imports inside it.
//
// A bad import — missing file, a cycle, or the depth cap — leaves that one
// "@import ..." line in place with a short inline error note, rather than
// discarding the whole file's content: one broken doc link should not take
// project instructions down with it.
func expandImports(content, dir string) string {
	return expandImportsDepth(content, dir, 0, nil)
}

func expandImportsDepth(content, dir string, depth int, stack []string) string {
	return importMacro.ReplaceAllStringFunc(content, func(match string) string {
		sm := importMacro.FindStringSubmatch(match)
		prefix, rel := sm[1], sm[2]

		abs, err := filepath.Abs(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return match + " (import error: " + err.Error() + ")"
		}
		for _, prev := range stack {
			if prev == abs {
				return match + " (import error: cycle importing " + rel + ")"
			}
		}
		if depth+1 > maxImportDepth {
			return match + " (import error: max depth exceeded)"
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return match + " (import error: " + err.Error() + ")"
		}
		expanded := expandImportsDepth(strings.TrimSpace(string(data)), filepath.Dir(abs), depth+1, append(stack, abs))
		return prefix + expanded
	})
}
