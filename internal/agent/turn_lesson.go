package agent

import (
	"strings"

	"github.com/orchestra/orchestra/internal/lessons"
)

// maxLessonVerifyBytes keeps one lesson's error summary from crowding out the
// rest of the file — the whole dept file is injected into later prompts.
const maxLessonVerifyBytes = 300

// recordTurnLesson appends an episodic anti-pattern lesson when a top-level
// turn ends with errors still on its ledger.
//
// Episodic learning used to run only for worker children, so the single agent
// mode most people actually use learned nothing from its own mistakes: across
// a 52-session field run the lessons directory was never even created. The
// signal counter behind lesson_promote already dedups entries and counts
// repeats across sessions, so appending once per errored turn is exactly what
// feeds it — a one-off writes a note and never promotes, while the same
// mistake three times becomes a promotion suggestion.
func (a *Agent) recordTurnLesson() {
	if a == nil || a.working == nil || a.tools == nil {
		return
	}
	// A run with no session has no continuity to learn into — one-shot
	// `apply` invocations would just accumulate orphan notes.
	if a.opts.SessionID == "" {
		return
	}
	// Worker and other child-only modes are recorded by tasks.recordWorkerLesson,
	// which knows the WorkOrder's dept and the verification outcome that this
	// layer cannot see. Recording here as well would double-write every failure.
	if IsChildOnlyMode(a.opts.Mode) {
		return
	}

	errs := a.working.Errors()
	if len(errs) == 0 {
		return
	}
	root := a.tools.WorkspaceRoot()
	if root == "" {
		return
	}

	dept := lessons.NormalizeDept("")
	verify := clipLessonText(strings.Join(errs, "; "), maxLessonVerifyBytes)
	task := a.working.Goal

	if err := lessons.Append(root, lessons.Entry{
		Dept:   dept,
		Kind:   lessons.KindAntiPattern,
		Task:   task,
		Files:  a.working.ActiveFiles(),
		Verify: verify,
	}); err != nil {
		return
	}
	lessons.BumpAntiPatternSignal(root, dept, lessons.AntiPatternKey(verify, task))
}

func clipLessonText(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
