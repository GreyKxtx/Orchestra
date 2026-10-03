package agent

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/orchestra/orchestra/llm"
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
// while "pkg/mcp" in a repository with no pkg/ is. known is what the
// conversation said before this answer: a path named there — a file a plan
// proposes, one the user mentioned — was not invented here.
func unknownWorkspacePaths(answer, workspaceRoot, known string) []string {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" || strings.TrimSpace(answer) == "" {
		return nil
	}
	text := fencedBlock.ReplaceAllString(stripThinkBlocks(answer), " ")

	type candidate struct {
		raw, rel   string
		start, end int
		exists     bool
	}
	var candidates []candidate
	for _, at := range pathCandidate.FindAllStringIndex(text, -1) {
		raw := text[at[0]:at[1]]
		if rel, ok := groundingCandidate(raw, text); ok {
			candidates = append(candidates, candidate{raw, rel, at[0], at[1], pathExistsIn(root, rel)})
		}
	}
	// listedWithReal: the candidate stands in one enumeration with a path that
	// exists ("internal/agent and pkg/mcp") — the answer is listing directories.
	listedWithReal := func(i int) bool {
		if i > 0 && candidates[i-1].exists && onlyListSeparator(text[candidates[i-1].end:candidates[i].start]) {
			return true
		}
		return i+1 < len(candidates) && candidates[i+1].exists &&
			onlyListSeparator(text[candidates[i].end:candidates[i+1].start])
	}

	seen := make(map[string]bool)
	var out []string
	for i, c := range candidates {
		raw, rel := c.raw, c.rel
		if seen[rel] {
			continue
		}
		seen[rel] = true
		if c.exists || !(listedWithReal(i) || markedAsPath(raw, rel, text)) {
			continue
		}
		if known != "" && strings.Contains(known, rel) {
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
	// The full stop ending a sentence is not part of the path.
	rel := strings.Trim(strings.TrimRight(raw, "."), "/")
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

// markedAsPath tells a path from two words joined by a slash. "pkg/mcp" and
// "combobox/dropdown" have the same shape, and a wrong complaint makes the
// model rewrite a correct answer, so a two-segment name counts only when
// something marks it as a path: code formatting, a dot (an extension, a dot
// directory), a third segment, or its first segment named as a directory again
// — the way an invented tree lists "pkg/" and "pkg/mcp". A real path elsewhere
// in the answer is not such a mark: "sample/eval" beside internal/agent/agent.go
// is still two words.
//
// Two capitalised words ("Build Tools/MinGW", "Canvas2D/WebGL", "TCP/IP") are a
// choice between names, not a directory, unless code formatting says otherwise.
func markedAsPath(raw, rel, text string) bool {
	if i := strings.Index(text, raw); i > 0 && text[i-1] == '`' {
		return true
	}
	if capitalisedAlternatives.MatchString(rel) {
		return false
	}
	if strings.Count(rel, "/") >= 2 || strings.Contains(rel, ".") {
		return true
	}
	first, _, _ := strings.Cut(rel, "/")
	return countDirMentions(text, first+"/") >= 2
}

// onlyListSeparator: the text between two names is nothing but the joint of a
// list — a comma, "and", "or", in English or Russian.
func onlyListSeparator(gap string) bool {
	w := strings.ToLower(strings.Trim(gap, " 	`*,;"))
	switch w {
	case "", "and", "or", "и", "или":
		return true
	}
	return false
}

var capitalisedAlternatives = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*/[A-Z][A-Za-z0-9]*$`)

// countDirMentions counts occurrences of dir that start a word.
func countDirMentions(text, dir string) int {
	n := 0
	for i := 0; ; {
		j := strings.Index(text[i:], dir)
		if j < 0 {
			return n
		}
		at := i + j
		if at == 0 || !isPathChar(text[at-1]) {
			n++
		}
		i = at + len(dir)
	}
}

func isPathChar(c byte) bool {
	return c == '_' || c == '-' || c == '.' || c == '/' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
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

// conversationMentions is the text of a conversation — every message and every
// tool call's arguments — for telling a path the answer repeats from one it
// made up.
func conversationMentions(history []llm.Message) string {
	var b strings.Builder
	for _, m := range history {
		b.WriteString(m.Content)
		b.WriteByte('\n')
		for _, tc := range m.ToolCalls {
			b.WriteString(string(tc.Function.Arguments))
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// rejectUngroundedFinal sends the answer back once when it describes parts of
// the workspace that are not there. Callers must have established that the turn
// mutated nothing, so a named path is a claim about the present, not a plan.
// a.finalHistory, set by finalStep, is the conversation the answer comes from.
func (a *Agent) rejectUngroundedFinal(visible string) (string, bool) {
	if a == nil || a.groundingCorrected || a.tools == nil {
		return "", false
	}
	if strings.TrimSpace(visible) == "" {
		return "", false
	}
	bad := unknownWorkspacePaths(visible, a.tools.WorkspaceRoot(), conversationMentions(a.finalHistory))
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
