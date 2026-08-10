package resolver

// levenshteinDistance returns the edit distance between two strings (runes).
func levenshteinDistance(a, b string) int {
	if a == b {
		return 0
	}
	ra := []rune(a)
	rb := []rune(b)
	na, nb := len(ra), len(rb)
	if na == 0 {
		return nb
	}
	if nb == 0 {
		return na
	}
	if na < nb {
		ra, rb = rb, ra
		na, nb = nb, na
	}

	prev := make([]int, nb+1)
	cur := make([]int, nb+1)
	for j := 0; j <= nb; j++ {
		prev[j] = j
	}
	for i := 1; i <= na; i++ {
		cur[0] = i
		for j := 1; j <= nb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := cur[j-1] + 1
			sub := prev[j-1] + cost
			cur[j] = minInt(del, minInt(ins, sub))
		}
		prev, cur = cur, prev
	}
	return prev[nb]
}

func lineSimilarity(a, b string) float64 {
	a = trimAnchorLine(a)
	b = trimAnchorLine(b)
	if a == b {
		return 1.0
	}
	maxLen := len([]rune(a))
	if lb := len([]rune(b)); lb > maxLen {
		maxLen = lb
	}
	if maxLen == 0 {
		return 1.0
	}
	d := levenshteinDistance(a, b)
	return 1.0 - float64(d)/float64(maxLen)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// fuzzyLineMinRatio is the per-line similarity floor for fuzzyBlockFind (E4).
const fuzzyLineMinRatio = 0.85

// anchorLineMinRatio is the similarity floor for first/last lines in fuzzyBlockFind.
const anchorLineMinRatio = 0.90

// fuzzyBlockFind matches first/last lines with bounded Levenshtein (≥ anchorLineMinRatio)
// and middle lines with ≥ fuzzyLineMinRatio. Same line count as needle. E4 pass 8.
func fuzzyBlockFind(haystack, needle string) (start, end, matches int) {
	needleLines := splitBlockLines(needle)
	if len(needleLines) < 3 {
		return 0, 0, 0
	}
	firstAnchor := trimAnchorLine(needleLines[0])
	lastAnchor := trimAnchorLine(needleLines[len(needleLines)-1])
	if firstAnchor == "" || lastAnchor == "" {
		return 0, 0, 0
	}

	hayLines, lineStarts := blockLinesAndOffsets(haystack)
	if len(hayLines) < len(needleLines) {
		return 0, 0, 0
	}

	for i := 0; i <= len(hayLines)-len(needleLines); i++ {
		if lineSimilarity(hayLines[i], needleLines[0]) < anchorLineMinRatio {
			continue
		}
		j := i + len(needleLines) - 1
		if lineSimilarity(hayLines[j], needleLines[len(needleLines)-1]) < anchorLineMinRatio {
			continue
		}
		ok := true
		for k := 1; k < len(needleLines)-1; k++ {
			if lineSimilarity(hayLines[i+k], needleLines[k]) < fuzzyLineMinRatio {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		// Skip windows already matched by strict block-anchor (exact first/last).
		if trimAnchorLine(hayLines[i]) == firstAnchor && trimAnchorLine(hayLines[j]) == lastAnchor {
			continue
		}
		matches++
		if matches == 1 {
			start = lineStarts[i]
			if j+1 < len(lineStarts) {
				end = lineStarts[j+1]
			} else {
				end = len(haystack)
			}
		}
		if matches > 1 {
			return start, end, matches
		}
	}
	return start, end, matches
}

// doubleAnchorFind uses the first two and last two non-empty trimmed lines as
// anchors. Middle lines are verbatim from the file. Phase 11 pass 9 (E4).
func doubleAnchorFind(haystack, needle string) (start, end, matches int) {
	needleLines := splitBlockLines(needle)
	needleAnchors := nonEmptyTrimmedLines(needleLines)
	if len(needleAnchors) < 4 {
		return 0, 0, 0
	}
	a1, a2 := needleAnchors[0], needleAnchors[1]
	b1, b2 := needleAnchors[len(needleAnchors)-2], needleAnchors[len(needleAnchors)-1]

	hayLines, lineStarts := blockLinesAndOffsets(haystack)
	if len(hayLines) < len(needleLines) {
		return 0, 0, 0
	}

	for i := 0; i <= len(hayLines)-len(needleLines); i++ {
		window := hayLines[i : i+len(needleLines)]
		winAnchors := nonEmptyTrimmedLines(window)
		if len(winAnchors) < 4 {
			continue
		}
		if winAnchors[0] != a1 || winAnchors[1] != a2 {
			continue
		}
		if winAnchors[len(winAnchors)-2] != b1 || winAnchors[len(winAnchors)-1] != b2 {
			continue
		}
		matches++
		j := i + len(needleLines) - 1
		if matches == 1 {
			start = lineStarts[i]
			if j+1 < len(lineStarts) {
				end = lineStarts[j+1]
			} else {
				end = len(haystack)
			}
		}
		if matches > 1 {
			return start, end, matches
		}
	}
	return start, end, matches
}

func nonEmptyTrimmedLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		t := trimAnchorLine(ln)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}
