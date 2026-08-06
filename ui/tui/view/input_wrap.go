package view

import (
	"strings"
	"unicode"

	runewidth "github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
)

type VisualRow struct {
	AbsStart     int
	Runes        []rune
	EndOfLogical bool
}

// VisualRows computes the word-wrapped breakdown of in.Value() at width w
// using the exact same algorithm as bubbles textarea's wrap (so our row
// boundaries align with bubbles' CursorUp/CursorDown / LineInfo). Result
// always has at least one row.
func (in Input) VisualRows(w int) []VisualRow {
	if w < 1 {
		w = 1
	}
	val := in.ta.Value()
	if val == "" {
		return []VisualRow{{AbsStart: 0, Runes: nil, EndOfLogical: true}}
	}
	var rows []VisualRow
	absOffset := 0
	for _, line := range strings.Split(val, "\n") {
		runes := []rune(line)
		if len(runes) == 0 {
			rows = append(rows, VisualRow{AbsStart: absOffset, Runes: nil, EndOfLogical: true})
		} else {
			starts := wordWrapStarts(runes, w)
			for i, s := range starts {
				end := len(runes)
				if i+1 < len(starts) {
					end = starts[i+1]
				}
				rows = append(rows, VisualRow{
					AbsStart:     absOffset + s,
					Runes:        runes[s:end],
					EndOfLogical: i == len(starts)-1,
				})
			}
		}
		absOffset += len(runes) + 1
	}
	if len(rows) == 0 {
		rows = append(rows, VisualRow{AbsStart: 0, Runes: nil, EndOfLogical: true})
	}
	return rows
}

// VisualLineCount returns the total number of visual rows the value occupies
// when soft-wrapped to the given width using bubbles' word-aware algorithm.
func (in Input) VisualLineCount(width int) int {
	return len(in.VisualRows(width))
}

// wordWrap is a verbatim port of bubbles textarea's internal wrap(): it
// produces the visual grid of a single logical line with word-aware breaks.
// Same algorithm → same row boundaries → cursor/selection overlays align
// with bubbles' own LineInfo / CursorUp / CursorDown.
//
// The grid may contain ONE synthetic trailing space at the very end of the
// last row (bubbles adds it for consistent cursor-at-end behaviour). All
// other runes in the grid appear in the same order as `runes` — see
// wordWrapStarts for how we recover input-rune offsets.
func wordWrap(runes []rune, width int) [][]rune {
	if width < 1 {
		width = 1
	}
	var (
		lines  = [][]rune{{}}
		word   = []rune{}
		row    int
		spaces int
	)
	for _, r := range runes {
		if unicode.IsSpace(r) {
			spaces++
		} else {
			word = append(word, r)
		}
		if spaces > 0 {
			if uniseg.StringWidth(string(lines[row]))+uniseg.StringWidth(string(word))+spaces > width {
				row++
				lines = append(lines, []rune{})
				lines[row] = append(lines[row], word...)
				lines[row] = append(lines[row], []rune(strings.Repeat(" ", spaces))...)
			} else {
				lines[row] = append(lines[row], word...)
				lines[row] = append(lines[row], []rune(strings.Repeat(" ", spaces))...)
			}
			spaces = 0
			word = nil
		} else {
			if len(word) == 0 {
				continue
			}
			lastCharLen := runewidth.RuneWidth(word[len(word)-1])
			if uniseg.StringWidth(string(word))+lastCharLen > width {
				if len(lines[row]) > 0 {
					row++
					lines = append(lines, []rune{})
				}
				lines[row] = append(lines[row], word...)
				word = nil
			}
		}
	}
	if uniseg.StringWidth(string(lines[row]))+uniseg.StringWidth(string(word))+spaces >= width {
		lines = append(lines, []rune{})
		lines[row+1] = append(lines[row+1], word...)
		spaces++
		lines[row+1] = append(lines[row+1], []rune(strings.Repeat(" ", spaces))...)
	} else {
		lines[row] = append(lines[row], word...)
		spaces++
		lines[row] = append(lines[row], []rune(strings.Repeat(" ", spaces))...)
	}
	return lines
}

// wordWrapStarts returns the rune indices (within `runes`) where each visual
// row begins after applying word-aware wrap at `width`. Always returns at
// least [0]. Built from wordWrap's grid: every non-last row contains exactly
// its input runes (no synthetic), so len(grid[i]) for i<N-1 is the count of
// input runes consumed by row i. The last row may have +1 synthetic trailing
// space — irrelevant for start offsets but caller must clamp slice bounds.
func wordWrapStarts(runes []rune, width int) []int {
	if len(runes) == 0 {
		return []int{0}
	}
	grid := wordWrap(runes, width)
	starts := make([]int, 0, len(grid))
	starts = append(starts, 0)
	consumed := 0
	for i := 0; i < len(grid)-1; i++ {
		consumed += len(grid[i])
		starts = append(starts, consumed)
	}
	return starts
}

// WrapWidth returns the wrap width bubbles textarea actually uses
// internally (= outer width minus prompt width minus borders). Use
// this for any wrap-aware computation (VisualLineCount, WelcomeRender
// chunking) so our row breakdown matches bubbles' own — otherwise the
// rendered cursor and the cursor bubbles moves with KeyUp/KeyDown
// disagree about which visual row the cursor sits on.
func (in Input) WrapWidth() int {
	return in.ta.Width()
}

// SyncHeight caps the textarea height to the visual line count given
// the current textarea wrap width, clamped to [1, max]. Call after any
// value mutation so the visible rows match the actual content (including
// soft-wrap, not just '\n' splits).
func (in *Input) SyncHeight(max int) {
	w := in.WrapWidth()
	if w < 1 {
		w = 80
	}
	h := in.VisualLineCount(w)
	if h < 1 {
		h = 1
	}
	if max < 1 {
		max = 1
	}
	if h > max {
		h = max
	}
	in.ta.SetHeight(h)
}

// mentionSpans returns [start,end) absolute rune ranges for @path tokens.
// A mention starts at '@' (start-of-text or after whitespace) and runs until
