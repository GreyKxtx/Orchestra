package fs

import (
	"fmt"
	"strings"
)

// A model that edits a file cannot see what it did.
//
// edit and write answered with a path and a sha256 and nothing else — no
// statement that the change landed, no sight of the result. So the model has
// to decide what to do next from a hash, and a common thing it decides is to
// say the change again in final.patches, which is how add_func ended up
// declaring Multiply twice.
//
// This builds what the tools say back instead: one line stating what changed,
// and the changed region with line numbers, in the same shape `read` uses so
// the model is looking at a familiar format. The whole file is deliberately
// not returned — a model that needs the whole file can read it, and echoing it
// after every edit would spend the context window on text the model already
// has.
const (
	// changedRegionContext is how many unchanged lines to keep either side, so
	// the model can see where in the file the change sits.
	changedRegionContext = 3
	// changedRegionMaxLines caps the snippet. A large rewrite is summarised
	// rather than replayed.
	changedRegionMaxLines = 40
)

// describeChange returns a one-line summary of how the file changed, and the
// changed region rendered with line numbers. Both are empty when nothing
// changed.
func describeChange(path, before, after string) (summary, region string) {
	beforeLines := splitLinesForDiff(before)
	afterLines := splitLinesForDiff(after)

	first := firstDifferingLine(beforeLines, afterLines)
	lastBefore, lastAfter := lastDifferingLines(beforeLines, afterLines, first)

	// Compared as lines rather than as bytes, so a file that differs only in
	// its line endings is not reported as a change on every line — and, more
	// importantly, so "nothing changed" is decided by the same walk that finds
	// the region, and the two can never disagree.
	if lastBefore < first && lastAfter < first {
		return fmt.Sprintf("%s is unchanged: the replacement matches what was already there", path), ""
	}

	removed := lastBefore - first + 1
	added := lastAfter - first + 1
	if removed < 0 {
		removed = 0
	}
	if added < 0 {
		added = 0
	}

	summary = fmt.Sprintf("%s written: %s at line %d", path, changeCounts(added, removed), first+1)
	region = renderRegion(afterLines, first, lastAfter)
	return summary, region
}

// changeCounts words the edit the way a reader of a diff would.
func changeCounts(added, removed int) string {
	switch {
	case removed == 0:
		return fmt.Sprintf("%s added", plural(added, "line"))
	case added == 0:
		return fmt.Sprintf("%s removed", plural(removed, "line"))
	default:
		return fmt.Sprintf("%s replaced by %s", plural(removed, "line"), plural(added, "line"))
	}
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// splitLinesForDiff splits content into lines, dropping the empty element a
// trailing newline produces so a file and the same file without its final
// newline do not differ by a phantom line.
func splitLinesForDiff(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

// firstDifferingLine returns the index of the first line that differs.
func firstDifferingLine(before, after []string) int {
	i := 0
	for i < len(before) && i < len(after) && before[i] == after[i] {
		i++
	}
	return i
}

// lastDifferingLines walks in from the end and returns the last differing line
// in each side. Either may be first-1, meaning that side only had lines added
// or removed at this point.
func lastDifferingLines(before, after []string, first int) (lastBefore, lastAfter int) {
	b, a := len(before)-1, len(after)-1
	for b >= first && a >= first && before[b] == after[a] {
		b--
		a--
	}
	return b, a
}

// renderRegion prints the changed lines of the new file with their numbers,
// plus a little unchanged context either side.
func renderRegion(lines []string, first, last int) string {
	if len(lines) == 0 {
		return ""
	}
	if last < first {
		// Lines were removed and none added. Show where the hole is.
		last = first
	}
	start := first - changedRegionContext
	if start < 0 {
		start = 0
	}
	end := last + changedRegionContext
	if end > len(lines)-1 {
		end = len(lines) - 1
	}
	if start > len(lines)-1 {
		return ""
	}

	truncated := false
	if end-start+1 > changedRegionMaxLines {
		end = start + changedRegionMaxLines - 1
		truncated = true
	}

	var b strings.Builder
	for i := start; i <= end; i++ {
		fmt.Fprintf(&b, "%d: %s\n", i+1, lines[i])
	}
	if truncated {
		fmt.Fprintf(&b, "… %d more changed line(s) not shown; read the file if you need them\n",
			last-end)
	}
	return b.String()
}
