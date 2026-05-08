package state

// InputHistory stores submitted messages for ↑↓ recall.
// Not goroutine-safe — only accessed from the Bubble Tea Update goroutine.
type InputHistory struct {
	entries []string
	maxSize int
	cursor  int    // index into entries from the end; -1 = not navigating
	draft   string // text saved when navigation started
}

// NewInputHistory creates a history with the given capacity.
func NewInputHistory(maxSize int) *InputHistory {
	return &InputHistory{maxSize: maxSize, cursor: -1}
}

// Push adds text to history. Consecutive duplicates are collapsed.
// If the history is full, the oldest entry is dropped.
func (h *InputHistory) Push(text string) {
	if text == "" {
		return
	}
	if len(h.entries) > 0 && h.entries[len(h.entries)-1] == text {
		h.cursor = -1
		return
	}
	h.entries = append(h.entries, text)
	if len(h.entries) > h.maxSize {
		h.entries = h.entries[1:]
	}
	h.cursor = -1
}

// Up moves to an older entry. current is the live input text (saved as draft
// on first call). Returns the entry to display.
func (h *InputHistory) Up(current string) string {
	if len(h.entries) == 0 {
		return current
	}
	if h.cursor == -1 {
		h.draft = current
		h.cursor = len(h.entries) - 1
	} else if h.cursor > 0 {
		h.cursor--
	}
	return h.entries[h.cursor]
}

// Down moves to a newer entry. When past the newest, returns the saved draft.
func (h *InputHistory) Down() string {
	if h.cursor == -1 {
		return ""
	}
	h.cursor++
	if h.cursor >= len(h.entries) {
		h.cursor = -1
		return h.draft
	}
	return h.entries[h.cursor]
}

// Reset clears navigation state without removing entries.
func (h *InputHistory) Reset() {
	h.cursor = -1
	h.draft = ""
}
