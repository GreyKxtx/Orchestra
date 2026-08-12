package state

// DiffReview is the state machine behind the diff-review panel: whether the
// diff block is inserted into the chat (shown), which file the review cursor
// points at, and the dry-run ops awaiting an explicit user apply. Keeping the
// transitions here (instead of loose App fields) makes cursor clamping and
// the pending-ops lifecycle unit-testable without the UI.
type DiffReview struct {
	shown  bool
	cursor int

	pendingOps []map[string]any
	pending    bool // ops must be confirmed by the user before apply
}

// Shown reports whether the diff block is currently inserted into the chat.
func (r *DiffReview) Shown() bool { return r.shown }

// SetShown mirrors an externally-driven visibility change (session restore,
// collapse toggle) without touching the cursor.
func (r *DiffReview) SetShown(shown bool) { r.shown = shown }

// Show marks the diff block as inserted and resets the cursor to the top.
func (r *DiffReview) Show() {
	r.shown = true
	r.cursor = 0
}

// Hide marks the diff block as removed.
func (r *DiffReview) Hide() { r.shown = false }

// Cursor returns the selected file index.
func (r *DiffReview) Cursor() int { return r.cursor }

// ResetCursor moves the cursor back to the first file.
func (r *DiffReview) ResetCursor() { r.cursor = 0 }

// Move shifts the cursor by delta within [0, fileCount) and reports whether
// the position changed.
func (r *DiffReview) Move(delta, fileCount int) bool {
	next := r.cursor + delta
	if next < 0 || next >= fileCount {
		return false
	}
	r.cursor = next
	return true
}

// Clamp forces the cursor into [0, fileCount) and returns it.
func (r *DiffReview) Clamp(fileCount int) int {
	if r.cursor < 0 {
		r.cursor = 0
	}
	if r.cursor >= fileCount {
		r.cursor = fileCount - 1
	}
	return r.cursor
}

// Arm stores dry-run ops that need user confirmation and enters review mode.
func (r *DiffReview) Arm(ops []map[string]any) {
	r.pendingOps = append([]map[string]any(nil), ops...)
	r.pending = true
}

// PendingReview reports whether the panel is in confirm-before-apply mode.
func (r *DiffReview) PendingReview() bool { return r.pending }

// PendingOps returns the ops awaiting confirmation.
func (r *DiffReview) PendingOps() []map[string]any { return r.pendingOps }

// HasPendingOps reports review mode with at least one op to apply.
func (r *DiffReview) HasPendingOps() bool { return r.pending && len(r.pendingOps) > 0 }

// ClearPending leaves review mode after a successful apply.
func (r *DiffReview) ClearPending() {
	r.pendingOps = nil
	r.pending = false
}

// Reset drops everything — rewind or discard of the whole review.
func (r *DiffReview) Reset() {
	r.pendingOps = nil
	r.pending = false
	r.shown = false
	r.cursor = 0
}
