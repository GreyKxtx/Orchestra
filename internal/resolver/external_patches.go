package resolver

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/orchestra/orchestra/internal/patches"
	"github.com/orchestra/orchestra/internal/ops"
	"github.com/orchestra/orchestra/internal/fsutil"
	"github.com/orchestra/orchestra/internal/protocol"
	"github.com/orchestra/orchestra/internal/cache"
)

// ResolveExternalPatches converts External Patch objects into Internal Ops v1.
//
// It intentionally produces strict ops and relies on the applier's strict+fuzzy policy
// for safe application.
func ResolveExternalPatches(projectRoot string, patchList []patches.Patch) ([]ops.AnyOp, error) {
	if strings.TrimSpace(projectRoot) == "" {
		return nil, fmt.Errorf("projectRoot is empty")
	}
	if len(patchList) == 0 {
		return nil, protocol.NewError(protocol.InvalidLLMOutput, "no patches provided", nil)
	}

	out := make([]ops.AnyOp, 0, len(patchList))

	for _, p := range patchList {
		path, perr := normalizeRelPath(p.Path)
		if perr != nil {
			return nil, perr
		}
		p.Path = path

		switch p.Type {
		case patches.TypeFileSearchReplace:
			op, err := resolveSearchReplace(projectRoot, p)
			if err != nil {
				return nil, err
			}
			rr := op
			out = append(out, ops.AnyOp{Op: rr.Op, Path: rr.Path, ReplaceRange: &rr})

		case patches.TypeFileUnifiedDiff:
			op, err := resolveUnifiedDiff(projectRoot, p)
			if err != nil {
				return nil, err
			}
			rr := op
			out = append(out, ops.AnyOp{Op: rr.Op, Path: rr.Path, ReplaceRange: &rr})

		case patches.TypeFileWriteAtomic:
			anyOp, err := resolveWriteAtomic(projectRoot, p)
			if err != nil {
				return nil, err
			}
			out = append(out, anyOp)

		default:
			return nil, protocol.NewError(protocol.InvalidLLMOutput, "unsupported patch type", map[string]any{
				"type": p.Type,
				"path": p.Path,
			})
		}
	}

	return out, nil
}

func resolveSearchReplace(projectRoot string, p patches.Patch) (ops.ReplaceRangeOp, error) {
	if strings.TrimSpace(p.Search) == "" && p.Search != "" {
		// Empty-but-present is allowed; treat as explicit empty search (unsupported).
	}
	if p.Search == "" {
		return ops.ReplaceRangeOp{}, protocol.NewError(protocol.InvalidLLMOutput, "search is empty", map[string]any{
			"path": p.Path,
			"type": p.Type,
		})
	}
	// replace can be empty (deletion).

	before, _, err := readFileOrEmpty(projectRoot, p.Path)
	if err != nil {
		return ops.ReplaceRangeOp{}, err
	}

	start, end, matches := findUnique(string(before), p.Search)
	if matches == 0 {
		// Forgiving fallback: retry ignoring trailing whitespace on every
		// line and CRLF/LF differences. The op we build still references
		// the *original* bytes via Expected, so the applier's strict
		// re-check semantics are unchanged.
		ltStart, ltEnd, ltMatches := lineTrimmedFind(string(before), p.Search)
		switch ltMatches {
		case 0:
			return ops.ReplaceRangeOp{}, protocol.NewError(protocol.StaleContent, "search block not found", map[string]any{
				"path":     p.Path,
				"search":   preview(p.Search, 200),
				"fileHash": cache.ComputeSHA256(before),
			})
		case 1:
			start, end = ltStart, ltEnd
			// Fall through to position computation below using the
			// recovered offsets. Expected is set from before[start:end].
		default: // >1
			return ops.ReplaceRangeOp{}, protocol.NewError(protocol.AmbiguousMatch, "search block is ambiguous (line-trimmed)", map[string]any{
				"path":    p.Path,
				"matches": ltMatches,
				"search":  preview(p.Search, 200),
			})
		}
	}
	if matches > 1 {
		return ops.ReplaceRangeOp{}, protocol.NewError(protocol.AmbiguousMatch, "search block is ambiguous", map[string]any{
			"path":    p.Path,
			"matches": matches,
			"search":  preview(p.Search, 200),
		})
	}

	startPos, err := posFromOffset(before, start)
	if err != nil {
		return ops.ReplaceRangeOp{}, protocol.NewError(protocol.InvalidLLMOutput, "failed to compute start position", map[string]any{
			"path":  p.Path,
			"error": err.Error(),
		})
	}
	endPos, err := posFromOffset(before, end)
	if err != nil {
		return ops.ReplaceRangeOp{}, protocol.NewError(protocol.InvalidLLMOutput, "failed to compute end position", map[string]any{
			"path":  p.Path,
			"error": err.Error(),
		})
	}

	return ops.ReplaceRangeOp{
		Op:   ops.OpFileReplaceRange,
		Path: p.Path,
		Range: ops.Range{
			Start: startPos,
			End:   endPos,
		},
		// Expected must be the *verbatim* bytes from the file at the
		// matched range (not p.Search): in the strict path they coincide,
		// but in the forgiving path the file's bytes carry trailing
		// whitespace / CRLF that p.Search lacks, and the applier's
		// strict re-check would otherwise reject.
		Expected:    string(before[start:end]),
		Replacement: p.Replace,
		Conditions: ops.Conditions{
			FileHash:    p.FileHash,
			AllowFuzzy:  true,
			FuzzyWindow: 2,
		},
	}, nil
}

func resolveUnifiedDiff(projectRoot string, p patches.Patch) (ops.ReplaceRangeOp, error) {
	if strings.TrimSpace(p.Diff) == "" {
		return ops.ReplaceRangeOp{}, protocol.NewError(protocol.InvalidLLMOutput, "diff is empty", map[string]any{
			"path": p.Path,
			"type": p.Type,
		})
	}

	before, _, err := readFileOrEmpty(projectRoot, p.Path)
	if err != nil {
		return ops.ReplaceRangeOp{}, err
	}

	after, err := applyUnifiedDiff(string(before), p.Diff)
	if err != nil {
		return ops.ReplaceRangeOp{}, protocol.NewError(protocol.StaleContent, "failed to apply unified diff", map[string]any{
			"path":  p.Path,
			"error": err.Error(),
		})
	}

	// Whole-file replacement is the simplest safe op for diffs.
	endPos, err := posFromOffset(before, len(before))
	if err != nil {
		return ops.ReplaceRangeOp{}, protocol.NewError(protocol.InvalidLLMOutput, "failed to compute file end position", map[string]any{
			"path":  p.Path,
			"error": err.Error(),
		})
	}

	return ops.ReplaceRangeOp{
		Op:   ops.OpFileReplaceRange,
		Path: p.Path,
		Range: ops.Range{
			Start: ops.Position{Line: 0, Col: 0},
			End:   endPos,
		},
		Expected:    string(before),
		Replacement: after,
		Conditions: ops.Conditions{
			FileHash:   p.FileHash,
			AllowFuzzy: false,
		},
	}, nil
}

func resolveWriteAtomic(projectRoot string, p patches.Patch) (ops.AnyOp, error) {
	_ = projectRoot // resolution is path-only; apply phase enforces workspace safety.

	// Guardrails: require at least one safety condition.
	mustNotExist := false
	condHash := ""
	if p.Conditions != nil {
		mustNotExist = p.Conditions.MustNotExist
		condHash = strings.TrimSpace(p.Conditions.FileHash)
	}
	// Back-compat: accept top-level file_hash for write_atomic if provided.
	if condHash == "" {
		condHash = strings.TrimSpace(p.FileHash)
	}
	if !mustNotExist && condHash == "" {
		return ops.AnyOp{}, protocol.NewError(protocol.InvalidLLMOutput, "write_atomic requires conditions.must_not_exist=true or conditions.file_hash", map[string]any{
			"path": p.Path,
			"type": p.Type,
		})
	}

	wa := ops.WriteAtomicOp{
		Op:      ops.OpFileWriteAtomic,
		Path:    p.Path,
		Content: p.Content,
		Mode:    p.Mode,
		Conditions: ops.WriteAtomicConditions{
			MustNotExist: mustNotExist,
			FileHash:     condHash,
		},
	}
	waCopy := wa
	return ops.AnyOp{Op: waCopy.Op, Path: waCopy.Path, WriteAtomic: &waCopy}, nil
}

func readFileOrEmpty(projectRoot, relPath string) ([]byte, string, error) {
	info, err := fsutil.ReadFile(projectRoot, filepath.FromSlash(relPath))
	if err != nil {
		// Path traversal gets mapped to a stable error code.
		if strings.Contains(err.Error(), "invalid file path") {
			return nil, "", protocol.NewError(protocol.PathTraversal, "path escapes workspace", map[string]any{"path": relPath})
		}
		if errors.Is(err, os.ErrNotExist) {
			return nil, cache.ComputeSHA256(nil), nil
		}
		return nil, "", err
	}
	b := []byte(info.Content)
	return b, cache.ComputeSHA256(b), nil
}

func normalizeRelPath(p string) (string, *protocol.Error) {
	p = filepath.ToSlash(strings.TrimSpace(p))
	if p == "" {
		return "", protocol.NewError(protocol.InvalidLLMOutput, "path is empty", nil)
	}
	p = filepath.Clean(filepath.FromSlash(p))
	p = filepath.ToSlash(p)
	if p == "." {
		return "", protocol.NewError(protocol.InvalidLLMOutput, "path is invalid", map[string]any{"path": p})
	}
	if p == ".." || strings.HasPrefix(p, "../") {
		return "", protocol.NewError(protocol.PathTraversal, "path escapes workspace", map[string]any{"path": p})
	}
	return p, nil
}

func preview(s string, max int) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "\n...(truncated)"
}

func findUnique(haystack string, needle string) (start int, end int, matches int) {
	if needle == "" {
		return 0, 0, 0
	}
	idx := 0
	for {
		j := strings.Index(haystack[idx:], needle)
		if j < 0 {
			break
		}
		matches++
		if matches == 1 {
			start = idx + j
			end = start + len(needle)
		}
		if matches > 1 {
			return start, end, matches
		}
		idx = idx + j + max(1, len(needle))
	}
	return start, end, matches
}

// lineTrimmedFind is a forgiving variant of findUnique that ignores trailing
// whitespace on every line and CRLF/LF differences. It is called only when
// strict findUnique returned 0 matches.
//
// Returns offsets into the *original* haystack so the resulting op stays
// byte-accurate against the on-disk file. Counts up to 2 occurrences:
// caller treats matches==0 as StaleContent and matches>1 as AmbiguousMatch,
// exactly like findUnique.
func lineTrimmedFind(haystack, needle string) (start, end, matches int) {
	if needle == "" {
		return 0, 0, 0
	}
	normHay, hayMap := normalizeTrailingWS(haystack)
	normNeedle, _ := normalizeTrailingWS(needle)
	if normNeedle == "" {
		return 0, 0, 0
	}

	idx := 0
	for {
		j := strings.Index(normHay[idx:], normNeedle)
		if j < 0 {
			break
		}
		matches++
		if matches == 1 {
			start = hayMap[idx+j]
			end = hayMap[idx+j+len(normNeedle)]
		}
		if matches > 1 {
			return start, end, matches
		}
		idx = idx + j + max(1, len(normNeedle))
	}
	return start, end, matches
}

// normalizeTrailingWS returns:
//   - normalized: a copy of s with `\r\n` collapsed to `\n` and trailing
//     `[ \t]+` removed from each line.
//   - origIdx: a slice of length len(normalized)+1 where origIdx[i] is the
//     byte offset in s that the normalized byte at position i was copied
//     from. origIdx[len(normalized)] == len(s) so callers can compute
//     end-of-match offsets cleanly.
func normalizeTrailingWS(s string) (normalized string, origIdx []int) {
	var b strings.Builder
	b.Grow(len(s))
	origIdx = make([]int, 0, len(s)+1)

	i := 0
	for i < len(s) {
		nl := strings.IndexByte(s[i:], '\n')
		var lineEnd int
		hasNL := false
		if nl < 0 {
			lineEnd = len(s)
		} else {
			lineEnd = i + nl
			hasNL = true
		}

		// Compute trim point: walk backwards over space/tab from lineEnd.
		// If the line ends with "\r\n", lineEnd points at the '\n', and we
		// must also skip the '\r' before trimming whitespace.
		contentEnd := lineEnd
		if hasNL && contentEnd > i && s[contentEnd-1] == '\r' {
			contentEnd-- // exclude the '\r' (collapse CRLF → LF)
		}
		for contentEnd > i {
			c := s[contentEnd-1]
			if c == ' ' || c == '\t' {
				contentEnd--
				continue
			}
			break
		}

		// Emit the trimmed line content with its original indices.
		for k := i; k < contentEnd; k++ {
			b.WriteByte(s[k])
			origIdx = append(origIdx, k)
		}
		// Emit the '\n' if there was one.
		if hasNL {
			b.WriteByte('\n')
			origIdx = append(origIdx, lineEnd) // record index of '\n' in s
			i = lineEnd + 1
		} else {
			i = lineEnd
		}
	}

	// Sentinel for end-of-string offset lookups.
	origIdx = append(origIdx, len(s))
	return b.String(), origIdx
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func posFromOffset(content []byte, off int) (ops.Position, error) {
	if off < 0 || off > len(content) {
		return ops.Position{}, fmt.Errorf("offset out of range")
	}
	lineStarts := computeLineStarts(content)
	// Find the last lineStart <= off.
	i := sort.Search(len(lineStarts), func(i int) bool { return lineStarts[i] > off })
	line := i - 1
	if line < 0 {
		line = 0
	}
	col := off - lineStarts[line]
	return ops.Position{Line: line, Col: col}, nil
}

func computeLineStarts(content []byte) []int {
	starts := make([]int, 0, 64)
	starts = append(starts, 0)
	for i := 0; i < len(content); i++ {
		if content[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// --- unified diff ---

type hunk struct {
	oldStart int
	oldCount int
	newStart int
	newCount int
	lines    []string
}

var hunkHeaderRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

func applyUnifiedDiff(original string, diffText string) (string, error) {
	original = strings.ReplaceAll(original, "\r\n", "\n")
	diffText = strings.ReplaceAll(diffText, "\r\n", "\n")

	origLines, origHasNL := splitLines(original)
	hunks, err := parseUnifiedDiff(diffText)
	if err != nil {
		return "", err
	}
	if len(hunks) == 0 {
		return "", fmt.Errorf("no hunks found")
	}

	var out []string
	origIdx := 0

	for _, h := range hunks {
		// Unified diff uses 1-based line numbers; for new files oldStart may be 0.
		target := h.oldStart - 1
		if h.oldStart == 0 {
			target = 0
		}
		if target < origIdx {
			return "", fmt.Errorf("hunk overlaps previous hunks")
		}
		if target > len(origLines) {
			return "", fmt.Errorf("hunk starts beyond end of file")
		}

		out = append(out, origLines[origIdx:target]...)
		origIdx = target

		for _, l := range h.lines {
			if l == "" {
				// empty context/add/remove line (prefix-only) is valid
				continue
			}
			prefix := l[0]
			body := ""
			if len(l) > 1 {
				body = l[1:]
			}
			switch prefix {
			case ' ':
				if origIdx >= len(origLines) {
					return "", fmt.Errorf("context exceeds file length")
				}
				if origLines[origIdx] != body {
					return "", fmt.Errorf("context mismatch at line %d", origIdx+1)
				}
				out = append(out, body)
				origIdx++
			case '-':
				if origIdx >= len(origLines) {
					return "", fmt.Errorf("remove exceeds file length")
				}
				if origLines[origIdx] != body {
					return "", fmt.Errorf("remove mismatch at line %d", origIdx+1)
				}
				origIdx++
			case '+':
				out = append(out, body)
			case '\\':
				// "\ No newline at end of file" - ignore
			default:
				return "", fmt.Errorf("invalid hunk line prefix: %q", prefix)
			}
		}
	}

	out = append(out, origLines[origIdx:]...)
	res := strings.Join(out, "\n")
	if origHasNL && res != "" {
		res += "\n"
	}
	// If original was empty and had no newline, keep it as-is.
	if origHasNL && original == "" && res == "" {
		res = "\n"
	}
	return res, nil
}

func parseUnifiedDiff(diffText string) ([]hunk, error) {
	lines := strings.Split(diffText, "\n")
	var hunks []hunk

	i := 0
	for i < len(lines) {
		line := lines[i]
		if !strings.HasPrefix(line, "@@") {
			i++
			continue
		}
		m := hunkHeaderRe.FindStringSubmatch(line)
		if len(m) == 0 {
			return nil, fmt.Errorf("invalid hunk header: %q", line)
		}
		oldStart := atoi(m[1])
		oldCount := 1
		if m[2] != "" {
			oldCount = atoi(m[2])
		}
		newStart := atoi(m[3])
		newCount := 1
		if m[4] != "" {
			newCount = atoi(m[4])
		}

		i++
		var hLines []string
		for i < len(lines) {
			if strings.HasPrefix(lines[i], "@@") {
				break
			}
			// Stop if next file header begins (rare when diff contains multiple files).
			if strings.HasPrefix(lines[i], "diff --git ") || strings.HasPrefix(lines[i], "--- ") || strings.HasPrefix(lines[i], "+++ ") {
				// treat as separator; next loop will skip until next @@
				break
			}
			hLines = append(hLines, lines[i])
			i++
		}

		hunks = append(hunks, hunk{
			oldStart: oldStart,
			oldCount: oldCount,
			newStart: newStart,
			newCount: newCount,
			lines:    hLines,
		})
		_ = oldCount
		_ = newCount
	}

	return hunks, nil
}

func splitLines(s string) (lines []string, endsWithNewline bool) {
	if strings.HasSuffix(s, "\n") {
		endsWithNewline = true
		s = strings.TrimSuffix(s, "\n")
	}
	if s == "" {
		return nil, endsWithNewline
	}
	return strings.Split(s, "\n"), endsWithNewline
}

func atoi(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + int(c-'0')
	}
	return n
}
