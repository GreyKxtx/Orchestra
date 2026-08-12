package view

import (
	"fmt"
	"hash/fnv"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/theme"
)

type lineKind int

const (
	lineContext lineKind = iota
	lineAdded
	lineRemoved
)

type diffLine struct {
	kind lineKind
	text string
}

// maxLCSCells caps the LCS DP table size at ~4 million cells (~32MB int).
// Beyond that, the O(n*m) algorithm becomes a perceptible UI hitch when the
// user toggles the diff view, so we degrade to a coarse "removed all / added
// all" rendering instead — still informative, never blocking.
const maxLCSCells = 4_000_000

// diffCache memoizes computeDiff results. During streaming, SetMessages
// re-renders an expanded diff on every flush (~10 fps); without the cache the
// O(n*m) LCS (up to ~32MB of DP table) re-runs on identical inputs, causing
// GC storms and visible stutter. Callers must treat the returned slice as
// read-only (they do — renderers only iterate it).
var (
	diffCacheMu   sync.Mutex
	diffCache     = map[uint64][]diffLine{}
	diffCacheKeys []uint64 // insertion order for cheap FIFO eviction
)

const diffCacheMax = 64

func diffCacheKey(a, b []string) uint64 {
	h := fnv.New64a()
	for _, s := range a {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	_, _ = h.Write([]byte{1})
	for _, s := range b {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	return h.Sum64()
}

// computeDiff returns an edit script between a and b using LCS, memoized by
// content hash. For huge files where the full DP table would be too
// expensive, falls back to a trivial "remove everything in a, add everything
// in b" diff.
func computeDiff(a, b []string) []diffLine {
	key := diffCacheKey(a, b)
	diffCacheMu.Lock()
	if cached, ok := diffCache[key]; ok {
		diffCacheMu.Unlock()
		return cached
	}
	diffCacheMu.Unlock()

	result := computeDiffUncached(a, b)

	diffCacheMu.Lock()
	if len(diffCacheKeys) >= diffCacheMax {
		evict := diffCacheKeys[0]
		diffCacheKeys = diffCacheKeys[1:]
		delete(diffCache, evict)
	}
	diffCache[key] = result
	diffCacheKeys = append(diffCacheKeys, key)
	diffCacheMu.Unlock()
	return result
}

func computeDiffUncached(a, b []string) []diffLine {
	n, m := len(a), len(b)
	if int64(n+1)*int64(m+1) > maxLCSCells {
		result := make([]diffLine, 0, n+m)
		for _, line := range a {
			result = append(result, diffLine{lineRemoved, line})
		}
		for _, line := range b {
			result = append(result, diffLine{lineAdded, line})
		}
		return result
	}
	// dp[i][j] = LCS length of a[:i] and b[:j]
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	// Backtrack.
	result := make([]diffLine, 0, n+m)
	i, j := n, m
	for i > 0 || j > 0 {
		switch {
		case i > 0 && j > 0 && a[i-1] == b[j-1]:
			result = append(result, diffLine{lineContext, a[i-1]})
			i--
			j--
		case j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]):
			result = append(result, diffLine{lineAdded, b[j-1]})
			j--
		default:
			result = append(result, diffLine{lineRemoved, a[i-1]})
			i--
		}
	}
	// Reverse in-place.
	for l, r := 0, len(result)-1; l < r; l, r = l+1, r-1 {
		result[l], result[r] = result[r], result[l]
	}
	return result
}

// filterContext removes context lines that are more than ctxN lines away from
// any change, inserting a "..." separator instead.
func filterContext(lines []diffLine, ctxN int) []diffLine {
	n := len(lines)
	keep := make([]bool, n)
	for i, l := range lines {
		if l.kind != lineContext {
			for d := -ctxN; d <= ctxN; d++ {
				if j := i + d; j >= 0 && j < n {
					keep[j] = true
				}
			}
		}
	}
	var out []diffLine
	skipping := false
	for i, l := range lines {
		if keep[i] {
			out = append(out, l)
			skipping = false
		} else if !skipping {
			out = append(out, diffLine{lineContext, "..."})
			skipping = true
		}
	}
	return out
}

func diffStyles() (add, del, ctx, path lipgloss.Style) {
	t := theme.CurrentTheme()
	return lipgloss.NewStyle().Foreground(t.Success()),
		lipgloss.NewStyle().Foreground(t.Error()),
		lipgloss.NewStyle().Foreground(t.TextMuted()),
		lipgloss.NewStyle().Foreground(t.Primary()).Bold(true)
}

// RenderFileDiff renders a colored unified-style diff of before→after.
// width is the available terminal width (unused in current implementation but
// reserved for future truncation).
func RenderFileDiff(before, after string, width int) string {
	aLines := strings.Split(strings.TrimRight(before, "\n"), "\n")
	bLines := strings.Split(strings.TrimRight(after, "\n"), "\n")
	dl := computeDiff(aLines, bLines)
	dl = filterContext(dl, 3)
	addStyle, delStyle, ctxStyle, _ := diffStyles()

	var sb strings.Builder
	for _, l := range dl {
		switch l.kind {
		case lineAdded:
			sb.WriteString(addStyle.Render("+ " + l.text))
		case lineRemoved:
			sb.WriteString(delStyle.Render("- " + l.text))
		case lineContext:
			if l.text == "..." {
				sb.WriteString(ctxStyle.Render("  ..."))
			} else {
				sb.WriteString(ctxStyle.Render("  " + l.text))
			}
		}
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// RenderAllDiffs renders diffs for a slice of FileDiff values, each with a
// path header. path and before/after fields are plain strings.
func RenderAllDiffs(diffs []FileDiffView, width int) string {
	_, _, ctxStyle, pathStyle := diffStyles()
	if len(diffs) == 0 {
		return ctxStyle.Render("(нет изменений в файлах)")
	}
	var sb strings.Builder
	for i, fd := range diffs {
		sb.WriteString(pathStyle.Render(fmt.Sprintf("── %s ──", fd.Path)))
		sb.WriteString("\n")
		sb.WriteString(RenderFileDiff(fd.Before, fd.After, width))
		if i < len(diffs)-1 {
			sb.WriteString("\n\n")
		}
	}
	return sb.String()
}

// FileDiffView is a view-layer copy of rpcclient.FileDiff to avoid import cycle.
type FileDiffView struct {
	Path         string
	Before       string
	After        string
	ReviewStatus string
	Selected     bool
}
