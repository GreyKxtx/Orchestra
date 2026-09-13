package agent

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/orchestra/orchestra/patch/patches"
)

// A turn can change a file two ways — the edit/write tools during the turn, and
// final.patches at the end of it — and the second is resolved against the
// result of the first. After an edit the model has not seen the file it just
// changed: the tool answers with a hash, not content. So any search_replace it
// then writes is necessarily anchored in the text it read BEFORE its own edit,
// and since an insertion leaves its anchor intact, that anchor still matches.
// The patch applies a second time and the turn reports success on a file that
// no longer compiles.
//
// This is not hypothetical. The eval task add_func failed on every run with
//
//	go_build: .\math.go:13:6: Multiply redeclared in this block
//
// from a three-call turn — read, edit, final — whose final patch restated the
// edit with one extra trailing newline in the search block.
//
// file_hash is the guard for exactly this case, and patches.Patch documents it
// as required for search_replace and unified_diff; nothing enforced it, and an
// absent hash skipped the check rather than failing it.

// recordMutatedPath notes that write/edit changed this file in the current
// turn. Only successful calls reach here.
func (a *Agent) recordMutatedPath(path string) {
	rel := normalizeRelPath(path)
	if rel == "" {
		return
	}
	if a.turnMutatedPaths == nil {
		a.turnMutatedPaths = make(map[string]bool, 4)
	}
	a.turnMutatedPaths[rel] = true
}

// restatedPatchHint refuses a final patch that re-describes a change this turn
// has already made, and returns the message the model should read instead.
//
// Enforcement is narrow on purpose: only for a path write/edit already changed,
// where a patch carrying no hash cannot have been planned against the current
// content. A model that re-reads the file gets a fresh hash in the read result
// and passes; a model recovering from a failed edit was never recorded here and
// passes too.
func (a *Agent) restatedPatchHint(finalPatches []patches.Patch) string {
	if len(finalPatches) == 0 || len(a.turnMutatedPaths) == 0 {
		return ""
	}

	var offenders []string
	seen := map[string]bool{}
	for _, p := range finalPatches {
		switch p.Type {
		case patches.TypeFileSearchReplace, patches.TypeFileUnifiedDiff:
		default:
			// write_atomic carries whole content, so it cannot double-apply.
			continue
		}
		if strings.TrimSpace(p.FileHash) != "" {
			continue
		}
		rel := normalizeRelPath(p.Path)
		if rel == "" || !a.turnMutatedPaths[rel] || seen[rel] {
			continue
		}
		seen[rel] = true
		offenders = append(offenders, rel)
	}
	if len(offenders) == 0 {
		return ""
	}

	list := strings.Join(offenders, ", ")
	return fmt.Sprintf(
		"You already changed %s with edit/write in this turn, and that change is kept. "+
			"The patch you just sent for %s is anchored in the text you read BEFORE that change, "+
			"so applying it would add the same code a second time and the file would stop compiling. "+
			"If the work is done, answer with {\"patches\":[]}. "+
			"If more is needed, read %s again and send a patch carrying the file_hash that read returns.",
		list, list, list)
}

// normalizeRelPath puts a workspace-relative path into the one spelling the
// comparison above can rely on.
func normalizeRelPath(p string) string {
	rel := strings.TrimSpace(filepath.ToSlash(p))
	rel = strings.TrimPrefix(rel, "./")
	return strings.Trim(rel, "/")
}
