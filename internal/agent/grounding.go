package agent

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// Grounding check: an answer that describes the workspace must not name parts
// of it that do not exist.
//
// Asking a model to "cite its sources" does not survive contact with a 9B: it
// will invent the citation alongside the claim. What does survive is checking
// the one class of claim that is mechanically checkable — a path — against the
// workspace itself. That catches the observed failure (a model that answered
// "what is this project" from a single README and described a pkg/ tree the
// repository has never had) without asking the model to be honest about it.

const (
	// groundingMaxReported keeps the correction short enough to be read.
	groundingMaxReported = 5
	// groundingMinCandidateLen filters noise like "a/b".
	groundingMinCandidateLen = 4
)

// fencedBlock matches ``` fenced code, where illustrative commands live
// ("mkdir pkg/newthing") and a path is not a claim about what exists.
var fencedBlock = regexp.MustCompile("(?s)```.*?```")

// pathCandidate is deliberately narrow: a workspace-relative path is segments
// of safe characters joined by forward slashes. Anything with a scheme, a
// drive letter or a leading slash is somebody else's filesystem.
var pathCandidate = regexp.MustCompile(`[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)+/?`)

// unknownWorkspacePaths returns the workspace-relative paths an answer names
// that do not exist, and whose directory does not exist either.
//
// The parent rule is what keeps this quiet: ".orchestra/plan.json" is a file a
// run creates, so a missing one inside a real directory is not a fabrication,
// while "pkg/mcp" in a repository with no pkg/ is.
func unknownWorkspacePaths(answer, workspaceRoot string) []string {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" || strings.TrimSpace(answer) == "" {
		return nil
	}
	text := fencedBlock.ReplaceAllString(stripThinkBlocks(answer), " ")

	seen := make(map[string]bool)
	var out []string
	for _, raw := range pathCandidate.FindAllString(text, -1) {
		rel, ok := groundingCandidate(raw, text)
		if !ok || seen[rel] {
			continue
		}
		seen[rel] = true
		if pathExistsIn(root, rel) {
			continue
		}
		// A missing file inside a directory that exists is not an invented
		// tree — only an unknown directory is.
		if dir := path.Dir(rel); dir != "." && pathExistsIn(root, dir) {
			continue
		}
		out = append(out, rel)
		if len(out) >= groundingMaxReported {
			break
		}
	}
	return out
}

// groundingCandidate normalises one match and rejects everything that is not a
// claim about this workspace.
func groundingCandidate(raw, text string) (string, bool) {
	rel := strings.Trim(raw, "/")
	if len(rel) < groundingMinCandidateLen || !strings.Contains(rel, "/") {
		return "", false
	}
	// A URL or a module path: the first segment names a host, not a directory.
	first, _, _ := strings.Cut(rel, "/")
	if strings.Contains(first, ".") && !strings.HasPrefix(first, ".") {
		return "", false
	}
	// Inside a longer token (a URL, a Windows path) the match is a fragment.
	if i := strings.Index(text, raw); i > 0 {
		switch text[i-1] {
		case '/', '\\', ':', '@':
			return "", false
		}
	}
	if strings.Contains(rel, "..") {
		return "", false
	}
	return rel, true
}

func pathExistsIn(root, rel string) bool {
	full := filepath.Join(root, filepath.FromSlash(rel))
	// Containment: a candidate that escapes the workspace is not ours to judge.
	if !strings.HasPrefix(full, filepath.Clean(root)) {
		return true
	}
	_, err := os.Stat(full)
	return err == nil
}

// rejectUngroundedFinal sends the answer back once when it describes parts of
// the workspace that are not there. Callers must have established that the turn
// mutated nothing, so a named path is a claim about the present, not a plan.
func (a *Agent) rejectUngroundedFinal(visible string) (string, bool) {
	if a == nil || a.groundingCorrected || a.tools == nil {
		return "", false
	}
	if strings.TrimSpace(visible) == "" {
		return "", false
	}
	bad := unknownWorkspacePaths(visible, a.tools.WorkspaceRoot())
	if len(bad) == 0 {
		return "", false
	}
	a.groundingCorrected = true
	return groundingHint(bad), true
}

// groundingHint is the correction handed back to the model. It names the paths
// rather than scolding in general terms, and points at the one call that
// answers the question properly.
func groundingHint(paths []string) string {
	return fmt.Sprintf(
		"Your answer names paths that do not exist in this workspace: %s. "+
			"Do not describe a structure you have not seen. Call repo_map (budget_bytes 2000-4000) or ls to check, "+
			"then answer again using only paths that appeared in a tool result — or drop those claims.",
		strings.Join(paths, ", "),
	)
}
