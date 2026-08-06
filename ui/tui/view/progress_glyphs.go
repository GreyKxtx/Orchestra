package view

// Progress glyph vocabulary shared by Tasks panel and Workflow progress.
// Right-side / stage indicators:
//
//	○  pending / waiting
//	⋯  in progress / running
//	✓  done
//	✗  failed / cancelled
//	↻  redo (workflow only)
const (
	ProgressPending = "○"
	ProgressRunning = "⋯"
	ProgressDone    = "✓"
	ProgressFailed  = "✗"
	ProgressRedo    = "↻"
)
