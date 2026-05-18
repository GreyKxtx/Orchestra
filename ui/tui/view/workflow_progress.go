package view

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/orchestra/orchestra/ui/tui/theme"
)

// WorkflowProgress is the sticky one-line widget rendered above the input
// while a workflow is in flight. It shows every stage seen so far with a
// glyph reflecting its current state, plus a live elapsed counter for the
// stage currently running:
//
//	▌ workflow:tdd_feature   ✓ spec  ✓ tests  ⋯ execute (12s)  · review
//
// Glyphs:
//
//	·   stage discovered but not yet attempted (or pending re-run after redo)
//	⋯   stage currently running — elapsed seconds in parentheses
//	✓   stage completed with an advance/accept marker
//	↻   stage completed with a redo:* action (we will re-run it)
//	✗   stage failed (action == "fail" or unknown action)
//
// The widget keeps stage order = insertion order. The first time we see a
// stage_start for a new id, it is appended to the list. Re-runs reuse the
// existing slot.
type WorkflowProgress struct {
	width int

	active       bool
	workflowName string

	stages []wpStage
	// idIndex maps stage_id → index in stages for O(1) updates as events arrive.
	idIndex map[string]int
}

type wpStage struct {
	id        string
	state     wpState
	startedAt time.Time
}

type wpState int

const (
	wpPending wpState = iota
	wpRunning
	wpDone
	wpRedo
	wpFail
)

// NewWorkflowProgress returns a widget initially inactive (Render returns "").
func NewWorkflowProgress(width int) *WorkflowProgress {
	return &WorkflowProgress{width: width, idIndex: map[string]int{}}
}

// SetSize updates the rendering width.
func (w *WorkflowProgress) SetSize(width int) { w.width = width }

// Active reports whether the widget should be drawn / consume layout rows.
func (w *WorkflowProgress) Active() bool { return w != nil && w.active }

// Begin starts (or resets) tracking for a workflow. Subsequent StageStart /
// StageDone calls accumulate state under the supplied name.
func (w *WorkflowProgress) Begin(workflowName string) {
	w.active = true
	w.workflowName = workflowName
	w.stages = w.stages[:0]
	for k := range w.idIndex {
		delete(w.idIndex, k)
	}
}

// StageStart marks the given stage as running. Appends a new slot the first
// time the id is seen. On a re-run (after StageDone with action "redo:*"),
// flips the existing slot back to running with a fresh timer.
func (w *WorkflowProgress) StageStart(stageID string) {
	if !w.active {
		w.active = true
	}
	now := time.Now()
	if i, ok := w.idIndex[stageID]; ok {
		w.stages[i].state = wpRunning
		w.stages[i].startedAt = now
		return
	}
	w.idIndex[stageID] = len(w.stages)
	w.stages = append(w.stages, wpStage{id: stageID, state: wpRunning, startedAt: now})
}

// StageDone marks the stage as completed. `action` follows the workflow
// runner's vocabulary: "advance" / "accept" / "redo:<id>" / "fail" / "" .
func (w *WorkflowProgress) StageDone(stageID, action string) {
	i, ok := w.idIndex[stageID]
	if !ok {
		// Receive done without a prior start — register the slot with the
		// terminal state we observed. Should not happen but keeps the
		// widget honest about whatever the core emits.
		w.idIndex[stageID] = len(w.stages)
		w.stages = append(w.stages, wpStage{id: stageID})
		i = w.idIndex[stageID]
	}
	switch {
	case strings.HasPrefix(action, "redo"):
		w.stages[i].state = wpRedo
	case action == "fail":
		w.stages[i].state = wpFail
	default:
		w.stages[i].state = wpDone
	}
}

// End hides the widget. Call when the workflow.run RPC returns.
func (w *WorkflowProgress) End() {
	w.active = false
	w.workflowName = ""
	w.stages = w.stages[:0]
	for k := range w.idIndex {
		delete(w.idIndex, k)
	}
}

// Render returns the one-line widget. Empty string when inactive — caller
// should not reserve a layout row in that case.
func (w *WorkflowProgress) Render() string {
	if !w.Active() {
		return ""
	}
	t := theme.CurrentTheme()
	bg := t.BackgroundSecondary()

	header := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Background(bg).
		Bold(true).
		Render("workflow:" + w.workflowName)

	mutedStyle := lipgloss.NewStyle().Background(bg).Foreground(t.TextMuted())
	textStyle := lipgloss.NewStyle().Background(bg).Foreground(t.Text())
	okStyle := lipgloss.NewStyle().Background(bg).Foreground(t.Success())
	runStyle := lipgloss.NewStyle().Background(bg).Foreground(t.Primary()).Bold(true)
	errStyle := lipgloss.NewStyle().Background(bg).Foreground(t.Error())
	warnStyle := lipgloss.NewStyle().Background(bg).Foreground(t.Warning())

	parts := []string{header, mutedStyle.Render("  ")}
	for i, s := range w.stages {
		switch s.state {
		case wpPending:
			parts = append(parts, mutedStyle.Render("· "+s.id))
		case wpRunning:
			elapsed := int(time.Since(s.startedAt).Round(time.Second).Seconds())
			parts = append(parts,
				runStyle.Render("⋯ "+s.id),
				mutedStyle.Render(fmt.Sprintf(" (%ds)", elapsed)),
			)
		case wpDone:
			parts = append(parts, okStyle.Render("✓ "+s.id))
		case wpRedo:
			parts = append(parts, warnStyle.Render("↻ "+s.id))
		case wpFail:
			parts = append(parts, errStyle.Render("✗ "+s.id))
		}
		if i < len(w.stages)-1 {
			parts = append(parts, textStyle.Render("  "))
		}
	}

	inner := lipgloss.JoinHorizontal(lipgloss.Top, parts...)

	w0 := w.width
	if w0 < 20 {
		w0 = 20
	}
	innerW := w0 - 2 // 1 border + 1 left pad

	// Truncate if the joined line exceeds available width; lipgloss handles
	// ANSI-aware width here.
	if lipgloss.Width(inner) > innerW {
		// Crude trim by visible width — drop bytes from the tail until it fits.
		// Stages are normally short (3-8 chars each), so this only fires in
		// pathological cases (many stages on a narrow terminal).
		runes := []rune(stripStyling(inner))
		for lipgloss.Width(string(runes)) > innerW && len(runes) > 0 {
			runes = runes[:len(runes)-1]
		}
		inner = textStyle.Render(string(runes) + "…")
	} else {
		// Pad to full width so the bg colour spans the row.
		pad := innerW - lipgloss.Width(inner)
		if pad > 0 {
			inner = inner + lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", pad))
		}
	}

	return lipgloss.NewStyle().
		Background(bg).
		Border(splitBorder, false, false, false, true).
		BorderForeground(t.Primary()).
		BorderBackground(bg).
		PaddingLeft(1).
		Width(w0).
		Render(inner)
}

// stripStyling drops ANSI escape sequences so a truncation pass operates on
// visible runes. Mirrors what a "visible width" trim helper would do — kept
// inline because we only need it on the rare overflow path.
func stripStyling(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		if r == 0x1b {
			in = true
			continue
		}
		if in {
			if r == 'm' {
				in = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
