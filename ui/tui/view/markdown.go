package view

import (
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// streamingPlainMaxRunes caps plain-text streaming render cost for very long replies.
const streamingPlainMaxRunes = 24_000

// glamourCache holds up to glamourCacheMax recently constructed term renderers
// keyed by word-wrap width. Building a fresh glamour.TermRenderer takes
// ~10-30ms because it loads + parses the stylesheet from embedded assets —
// and renderMarkdown is called once per assistant message per SetMessages
// call (i.e. per UI tick during streaming). Caching it makes the hot path
// orders of magnitude faster.
//
// We keep more than one entry so a transient resize (or two viewports with
// different widths) doesn't trash the cache on every flip.
const glamourCacheMax = 4

type glamourEntry struct {
	width int
	rdr   *glamour.TermRenderer
}

var (
	glamourMu      sync.Mutex
	glamourEntries []glamourEntry
)

// renderMarkdown renders text through glamour with dark styling. Returns the
// rendered text trimmed of leading/trailing whitespace. On any renderer
// construction error, falls back to the original plain text.
func renderMarkdown(text string, width int) string {
	if width < 10 {
		width = 10
	}
	r := acquireGlamour(width)
	if r == nil {
		return text
	}
	out, err := r.Render(text)
	if err != nil {
		return text
	}
	return strings.TrimSpace(out)
}

// renderPlainStreamingText renders in-flight assistant prose without glamour.
// Full markdown runs once when Streaming=false (FinishAssistant).
func renderPlainStreamingText(text string, width int) string {
	if width < 10 {
		width = 10
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) > streamingPlainMaxRunes {
		text = "…" + string(runes[len(runes)-streamingPlainMaxRunes:])
	}
	return lipgloss.NewStyle().Width(width).Render(text)
}

// acquireGlamour returns a cached renderer for the given word-wrap width,
// constructing a new one (and evicting the oldest entry when capacity is full)
// on a cache miss.
func acquireGlamour(width int) *glamour.TermRenderer {
	glamourMu.Lock()
	defer glamourMu.Unlock()
	for i, e := range glamourEntries {
		if e.width == width {
			// LRU move-to-back so frequent widths survive eviction.
			glamourEntries = append(append(glamourEntries[:i], glamourEntries[i+1:]...), e)
			return e.rdr
		}
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStylePath("dark"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil
	}
	glamourEntries = append(glamourEntries, glamourEntry{width: width, rdr: r})
	if len(glamourEntries) > glamourCacheMax {
		glamourEntries = glamourEntries[len(glamourEntries)-glamourCacheMax:]
	}
	return r
}
