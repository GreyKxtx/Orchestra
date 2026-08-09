package resolver

import (
	"strings"
)

// lineRangeBytes returns byte offsets for 1-based inclusive lineStart..lineEnd.
func lineRangeBytes(s string, lineStart, lineEnd int) (start, end int, ok bool) {
	if lineStart < 1 || lineEnd < lineStart {
		return 0, 0, false
	}
	line := 1
	byteStart := 0
	rangeStart := -1
	rangeEnd := len(s)
	for i := 0; i <= len(s); i++ {
		if line == lineStart && rangeStart < 0 {
			rangeStart = byteStart
		}
		if i == len(s) {
			break
		}
		if s[i] == '\n' {
			if line == lineEnd {
				rangeEnd = i + 1
				break
			}
			line++
			byteStart = i + 1
		}
	}
	if rangeStart < 0 || line < lineEnd {
		return 0, 0, false
	}
	return rangeStart, rangeEnd, true
}

// ApplySearchReplaceWithScope applies search→replace only within lineStart..lineEnd
// (1-based inclusive). Ambiguity is evaluated inside the scope, not the whole file.
func ApplySearchReplaceWithScope(content []byte, search, replace string, lineStart, lineEnd int) ([]byte, error) {
	s := string(content)
	rs, re, ok := lineRangeBytes(s, lineStart, lineEnd)
	if !ok {
		return ApplySearchReplace(content, search, replace)
	}
	scoped := s[rs:re]
	newScoped, err := ApplySearchReplace([]byte(scoped), search, replace)
	if err != nil {
		return nil, err
	}
	var buf strings.Builder
	buf.WriteString(s[:rs])
	buf.WriteString(string(newScoped))
	buf.WriteString(s[re:])
	return []byte(buf.String()), nil
}
