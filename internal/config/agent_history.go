package config

import "strings"

// ResolvedToolDigestBytes returns max raw tool output kept in history (0 = disabled).
func (a AgentConfig) ResolvedToolDigestBytes() int {
	if a.ToolDigestKB < 0 {
		return 0
	}
	kb := a.ToolDigestKB
	if kb == 0 {
		kb = 48
	}
	return kb * 1024
}

// ResolvedAutoSessionMemory reports whether explore/grep auto-notes session memory.
func (a AgentConfig) ResolvedAutoSessionMemory() bool {
	if a.AutoSessionMemory == nil {
		return true
	}
	return *a.AutoSessionMemory
}

// ResolvedHistoryPruneKeepRecent returns how many recent tool atoms stay full
// during retroactive prune (and survive a compaction verbatim).
func (a AgentConfig) ResolvedHistoryPruneKeepRecent() int {
	if a.HistoryPruneKeepRecent <= 0 {
		return DefaultHistoryPruneKeepRecent
	}
	return a.HistoryPruneKeepRecent
}

// DefaultHistoryPruneKeepRecent mirrors agent/history.DefaultHistoryPruneKeepRecent.
const DefaultHistoryPruneKeepRecent = 6

// ResolvedChildMaxSteps returns the clamp for task/task_spawn child MaxSteps
// (default DefaultChildMaxSteps).
func (a AgentConfig) ResolvedChildMaxSteps() int {
	if a.ChildMaxSteps <= 0 {
		return DefaultChildMaxSteps
	}
	return a.ChildMaxSteps
}

// DefaultChildMaxSteps caps a child agent's loop. 12 was too tight for a
// worker that has to read, edit and then validate its own change: it ran out
// of steps mid-task and returned a partial result the lead had to redo.
const DefaultChildMaxSteps = 24

// ResolvedBytesPerContextToken returns the estimate calibration (default 4).
func (a AgentConfig) ResolvedBytesPerContextToken() int {
	if a.BytesPerContextToken <= 0 {
		return 4
	}
	return a.BytesPerContextToken
}

// ResolvedAutoSummaryMemory reports whether long turns auto-write a project memory summary.
func (a AgentConfig) ResolvedAutoSummaryMemory() bool {
	if a.AutoSummaryMemory == nil {
		return true
	}
	return *a.AutoSummaryMemory
}

// ResolvedCompactThresholdPct returns the compact trigger percent for callers
// that do not know the model context window: negative → 0 (disabled),
// 0 → LegacyCompactThresholdPct, positive → as-is.
//
// Prefer ProjectConfig.EffectiveCompactThresholdPct, which resolves the 0 =
// auto case against the real window instead of the conservative legacy value.
func (a AgentConfig) ResolvedCompactThresholdPct() int {
	if a.CompactThresholdPct < 0 {
		return 0
	}
	if a.CompactThresholdPct == 0 {
		return LegacyCompactThresholdPct
	}
	return a.CompactThresholdPct
}

// LegacyCompactThresholdPct is the pre-auto fixed trigger. It is right for a
// small local window and far too early for a 100k+ one: on a 200k model it
// compacts away most of the history the agent could still be holding.
const LegacyCompactThresholdPct = 60

// AutoCompactThresholdPct returns the compaction trigger as a percentage of
// the prompt byte budget, scaled to the model window.
//
// Compaction is lossy — it replaces transcript with a summary — so it should
// fire as late as the window safely allows. On a small window the reserve has
// to be generous because a single tool result can be a large fraction of it;
// on a 100k+ window the same absolute reserve is a rounding error, and firing
// at 60% throws away tens of thousands of tokens of usable working memory.
func AutoCompactThresholdPct(ctxTokens int) int {
	switch {
	case ctxTokens <= 0:
		// Unknown window: MaxPromptBytes is probably the flat limits.context_kb
		// default, so stay conservative.
		return LegacyCompactThresholdPct
	case ctxTokens < 32000:
		return LegacyCompactThresholdPct
	case ctxTokens < 100000:
		return 75
	default:
		return 85
	}
}

// ResolvedWorkingState reports whether <working_state> inject is enabled (default true).
func (a AgentConfig) ResolvedWorkingState() bool {
	if a.WorkingState == nil {
		return true
	}
	return *a.WorkingState
}

// ResolvedTurnDigestKeep returns how many turn digests to inject (default 3).
// nil → 3; 0 or negative → off (no persist/inject).
func (a AgentConfig) ResolvedTurnDigestKeep() int {
	if a.TurnDigestKeep == nil {
		return 3
	}
	if *a.TurnDigestKeep <= 0 {
		return 0
	}
	return *a.TurnDigestKeep
}

// ResolvedTurnDigestEveryN returns mid-run micro-digest interval (default 6).
// nil → 6; 0 or negative → end-of-run only.
func (a AgentConfig) ResolvedTurnDigestEveryN() int {
	if a.TurnDigestEveryN == nil {
		return 6
	}
	if *a.TurnDigestEveryN <= 0 {
		return 0
	}
	return *a.TurnDigestEveryN
}

// ResolvedAutoIndex reports whether the embedding index is built in the
// background at core start.
//
// Defaults to on once a model is configured: naming an embedding model is
// already the opt-in, and an index that is only built when someone remembers
// `orchestra ckg embed` is an index that stays empty — which makes
// semantic_search look broken rather than unprepared. Indexing is incremental,
// so an unchanged repo costs nothing; set false for a paid endpoint where even
// the first pass is unwelcome.
func (e EmbedConfig) ResolvedAutoIndex() bool {
	if strings.TrimSpace(e.Model) == "" {
		return false
	}
	if e.AutoIndex == nil {
		return true
	}
	return *e.AutoIndex
}

// ResolvedSemanticAutoExplore reports whether semantic_search auto-runs explore on top hits.
func (e EmbedConfig) ResolvedSemanticAutoExplore() bool {
	if e.Model == "" {
		return false
	}
	if e.SemanticAutoExplore == nil {
		return true
	}
	return *e.SemanticAutoExplore
}

// ResolvedSemanticAutoExploreTopK returns explore enrichment count for semantic_search.
func (e EmbedConfig) ResolvedSemanticAutoExploreTopK() int {
	if e.SemanticAutoExploreTopK <= 0 {
		return 2
	}
	if e.SemanticAutoExploreTopK > 5 {
		return 5
	}
	return e.SemanticAutoExploreTopK
}
