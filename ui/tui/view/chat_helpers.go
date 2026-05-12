package view

import (
	"fmt"
	"strings"
	"time"
)

// stripFinalEnvelope removes any balanced JSON object containing a `"patches"`
// key from the assistant text. The agent emits final-action JSON (e.g.
// `{"type":"final","final":{"patches":[...]}}`) which streams into chat as
// plain text — we hide it because the diff is already shown via a dedicated
// diff message.
func stripFinalEnvelope(text string) string {
	for {
		i := strings.IndexByte(text, '{')
		if i < 0 {
			return text
		}
		end := matchJSONObject(text, i)
		if end < 0 {
			return text
		}
		blob := text[i : end+1]
		if strings.Contains(blob, `"patches"`) {
			text = strings.TrimSpace(text[:i] + text[end+1:])
			continue
		}
		text = text[:end+1] + stripFinalEnvelope(text[end+1:])
		return text
	}
}

// matchJSONObject returns the index of the closing `}` that balances the
// opening `{` at start, respecting string literals and escapes. Returns -1
// when no balanced match is found (truncated/malformed JSON).
func matchJSONObject(s string, start int) int {
	depth, inStr, esc := 0, false, false
	for j := start; j < len(s); j++ {
		c := s[j]
		if esc {
			esc = false
			continue
		}
		switch {
		case c == '\\':
			esc = true
		case c == '"':
			inStr = !inStr
		case inStr:
			// inside string literal — ignore braces
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return -1
}

// formatDuration prints a compact duration: 800ms → "0.8s", 65s → "1m 5s".
func formatDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		m := int(d / time.Minute)
		s := int((d % time.Minute) / time.Second)
		return fmt.Sprintf("%dm %ds", m, s)
	}
}

// formatTokens prints "12.3k tokens" / "532 tokens" / "1.2m tokens".
func formatTokens(n int) string { return formatCount(n) + " tokens" }

// formatCount renders an integer count using k/m suffixes — shared by every
// chat/footer/statusbar caller.
func formatCount(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fm", float64(n)/1_000_000)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// titlecase uppercases the first ASCII letter of s, leaving the rest intact.
func titlecase(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
