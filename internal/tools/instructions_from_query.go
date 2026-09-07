package tools

import (
	"os"
	"path/filepath"
	"strings"
)

// atRefPattern matches an @-reference to a workspace file: "@pkg/auth/token.go".
//
// It requires a separator, which is what keeps "user@example.com" and a bare
// "@decorator" out — an @-reference to a file the model should read about names
// a path, and a path inside a workspace has a directory in it. That also means
// a mention of a root-level file ("@README.md") contributes nothing here, which
// is correct: its directory is the workspace root, whose instructions are
// already injected as the orchestra layer.
const atRefSeparators = `/\`

// atRefPaths extracts the workspace-relative @-references from a query, in the
// order they appear, as slash-separated paths.
//
// Attachments reach the agent as @-references too — attachments.MergeQueryWithFileRefs
// appends "@<path>" for every attached file — so this one extractor covers both
// what the user typed and what they attached.
//
// Absolute paths and anything climbing out with ".." are dropped: they are not
// references into this workspace, and the instruction walk must not be pointed
// outside it.
func atRefPaths(query string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, field := range strings.Fields(query) {
		// Strip punctuation the sentence owns, not the path: "(@a/b.go)," .
		field = strings.Trim(field, "(),;:\"'`")
		if !strings.HasPrefix(field, "@") {
			continue
		}
		ref := strings.TrimPrefix(field, "@")
		// A trailing period is sentence punctuation unless it is an extension
		// separator with something after it.
		ref = strings.TrimRight(ref, ".")
		if ref == "" || !strings.ContainsAny(ref, atRefSeparators) {
			continue
		}
		ref = filepath.ToSlash(ref)
		if strings.HasPrefix(ref, "/") || strings.Contains(ref, "..") {
			continue
		}
		// A Windows drive letter ("C:/x") is absolute too.
		if len(ref) > 1 && ref[1] == ':' {
			continue
		}
		if seen[ref] {
			continue
		}
		seen[ref] = true
		out = append(out, ref)
	}
	return out
}

// refDir resolves an @-reference to the directory whose instructions it is
// asking for.
//
// People mention directories as often as files — "look at @pkg/auth" — and
// taking the parent of whatever was named loaded the mentioned package's
// PARENT and skipped the package itself, which is the one thing the mention
// was about. A path that exists and is a directory is the answer; anything
// else — a file, or a path that does not exist yet because the user is asking
// for it to be created — resolves to its parent.
func refDir(workspaceRoot, ref string) string {
	full := filepath.Join(workspaceRoot, filepath.FromSlash(ref))
	if st, err := os.Stat(full); err == nil && st.IsDir() {
		return full
	}
	return filepath.Dir(full)
}

// InstructionsForQuery returns the nested ORCHESTRA.md text for the directories
// of every file the query references, ready to append to the system prompt.
//
// Nested instructions used to load only when a tool read a file
// (discoverInstructions from fs.read), so a package's rules arrived one step
// late — or never, when the model answered from the mention alone without
// opening anything. An @-mention or an attachment is a statement about which
// code the turn is about, and that is exactly when its rules are worth having.
//
// It reuses discoverInstructions, so the walk, the workspace bound and the
// per-turn seen-set are shared: a directory loaded here is not offered again
// when the model then reads the file.
func (r *Runner) InstructionsForQuery(query string) string {
	if r == nil {
		return ""
	}
	refs := atRefPaths(query)
	if len(refs) == 0 {
		return ""
	}
	var parts []string
	for _, ref := range refs {
		if text := r.discoverInstructions(refDir(r.workspaceRoot, ref)); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n---\n\n")
}
