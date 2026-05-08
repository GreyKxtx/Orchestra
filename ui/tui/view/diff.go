package view

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
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

// computeDiff returns an edit script between a and b using LCS.
func computeDiff(a, b []string) []diffLine {
	n, m := len(a), len(b)
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

var (
	addStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#9ece6a"))
	delStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#f7768e"))
	ctxStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#565f89"))
	pathStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7aa2f7")).Bold(true)
)

// RenderFileDiff renders a colored unified-style diff of before→after.
// width is the available terminal width (unused in current implementation but
// reserved for future truncation).
func RenderFileDiff(before, after string, width int) string {
	aLines := strings.Split(strings.TrimRight(before, "\n"), "\n")
	bLines := strings.Split(strings.TrimRight(after, "\n"), "\n")
	dl := computeDiff(aLines, bLines)
	dl = filterContext(dl, 3)

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
	if len(diffs) == 0 {
		return ctxStyle.Render("(no file changes)")
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
	Path   string
	Before string
	After  string
}
